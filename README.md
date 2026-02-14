# ParaSpeech

统一语音中间件 — 将 STT 与 TTS 合并为单一组件。

单进程、双通道、gRPC + CLI 双入口、零密钥暴露。

---

## 项目架构

```
              调用方（OpenClaw / 外部服务）
                       │
          ┌────────────┴────────────┐
          │    gRPC :9800           │   CLI 子命令
          │   （当前 unary）         │  （委托 gRPC）
          └────────────┬────────────┘
                       │
    ┌──────────────────┴──────────────────┐
    │           paraspeech serve          │
    │  ┌─────────┐       ┌─────────┐     │
    │  │ STT 通道│       │ TTS 通道│     │
    │  │ decode  │       │sanitize │     │
    │  │ → VAD   │       │→ split  │     │
    │  │ → trim  │       │→ synth  │     │
    │  │→upstream│       │→ concat │     │
    │  └────┬────┘       └────┬────┘     │
    │       │      密钥金库    │          │
    │       └──────┤vault├────┘          │
    └──────────────┴─────┴───────────────┘
                       │
            OpenAI / Deepgram / Edge
```

### 核心分层

| 层 | 角色 | 关键职责 | 约束 |
|----|------|----------|------|
| Transport | `grpc` / `cli` | 协议入口、参数接收、输出格式化 | 不做业务决策，不直接调上游 |
| Service | `stt` / `tts` | 编排完整业务链路 | 仅依赖 domain/infra/provider 抽象 |
| Domain | `codec` / `vad` / `voice` / `queue` | 语音处理与策略算法 | 与具体上游 API 解耦 |
| Provider | `provider/openai` | 封装上游 HTTP 协议细节 | 不关心调用来源（CLI/gRPC） |
| Infra | `config` / `vault` / `observe` / `errs` | 配置、密钥、日志、错误模型 | 全局可复用，避免业务入侵 |

依赖方向固定：`Transport -> Service -> Domain/Provider/Infra`。

### 业务链路（STT）

1. 客户端调用 `STTService.Transcribe` 或 `paraspeech transcribe`。
2. 服务先将任意输入音频统一解码为 `16kHz / mono / s16le`。
3. 若 `vad.mode != off`，执行 VAD 裁剪并生成 VAD 元数据。
4. 将 PCM 封装为标准 WAV 后上传 OpenAI STT。
5. 返回文本与元信息（trace/process/vad），CLI 默认只输出 `text`。

设计要点：

- 输入格式不可信，先统一解码再处理。
- 上游识别以 WAV 容器作为稳定交付格式。
- VAD 失败时走回退，不阻断主链路。

### 业务链路（TTS）

1. 客户端调用 `TTSService.Synthesize` 或 `paraspeech synthesize`。
2. 文本先做 sanitize（去 markdown/代码块/URL 噪声）。
3. 按时长和语义边界分段，避免句中硬切。
4. 逐段调用 OpenAI TTS 并聚合输出。
5. 返回段信息与音频结果（CLI 侧默认精简输出）。

设计要点：

- 清洗与分段是稳定性的关键，不把脏输入直接交给上游。
- 多段聚合保证长文本场景可控。

### 运行边界与安全模型

1. `paraspeech serve` 是唯一持有密钥的进程。
2. CLI 仅通过本地 gRPC 委托，不直接触达上游。
3. 密钥文件以 `root:paraspeech` + `0640` 权限管理。
4. 支持 `systemctl reload` 热重载密钥，旧密钥内存会清零。

### 当前接口实现状态

- 已实现：`STTService.Transcribe`（unary）
- 已实现：`TTSService.Synthesize`（unary）
- 已实现：`TTSService.Preview`（unary，返回真实清洗+分段结果）
- 已实现：`HealthService.Check`（unary）
- 未实现：`STTService.TranscribeStream`、`TTSService.SynthesizeStream`（调用会返回 `Unimplemented`）

### 实现映射（最小）

只列核心映射，便于定位逻辑：

- STT 编排：`internal/stt/service.go`
- TTS 编排：`internal/tts/service.go`
- gRPC 入口：`internal/transport/grpc/`
- OpenAI 对接：`internal/provider/openai/`
- 密钥管理：`internal/vault/vault.go`
- 配置模型：`internal/config/config.go`
- 对外协议：`api/proto/paraspeech/v1/*.proto`

---

## 构建

**前置依赖**：Go 1.22+、ffmpeg 7.x、make

```bash
# 编译
make build          # → ./paraspeech

# 测试
make test           # go test ./internal/... -race -cover

# 静态检查
make lint           # golangci-lint（需预装）

# Proto 生成（需 buf CLI）
make proto
```

---

## 部署

### 1. 创建专用用户

```bash
sudo useradd -r -s /usr/sbin/nologin paraspeech
```

### 2. 安装二进制与配置

```bash
sudo install -m 755 paraspeech /usr/local/bin/paraspeech

sudo mkdir -p /etc/paraspeech
sudo cp paraspeech.toml /etc/paraspeech/
sudo chmod 644 /etc/paraspeech/paraspeech.toml
```

### 3. 配置密钥文件

```bash
sudo touch /etc/paraspeech/secrets.env
sudo chown root:paraspeech /etc/paraspeech/secrets.env
sudo chmod 640 /etc/paraspeech/secrets.env
sudo vim /etc/paraspeech/secrets.env
```

内容格式：

```env
PARASPEECH_STT_KEY=sk-xxx
PARASPEECH_TTS_KEY=sk-xxx
```

### 4. 验证权限隔离

```bash
# OpenClaw 用户不可读（必须返回 Permission denied）
sudo -u jacyl4 cat /etc/paraspeech/secrets.env

# paraspeech 用户可读
sudo -u paraspeech cat /etc/paraspeech/secrets.env
```

### 5. 安装并启动 systemd 服务

```bash
sudo cp configs/paraspeech.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now paraspeech
```

### 6. 验证服务

```bash
paraspeech health
```

### 密钥轮换（零停机）

```bash
sudo vim /etc/paraspeech/secrets.env     # 编辑新密钥
sudo systemctl reload paraspeech         # SIGHUP 热重载
```

热重载流程：权限预检 → 解析 → mlock → 原子替换 → memzero 旧密钥。校验失败时保留旧密钥继续服务。

---

## 配置

主配置文件 `/etc/paraspeech/paraspeech.toml`，完整模板见 `paraspeech.toml`。

关键配置项：

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `server.grpc_addr` | `127.0.0.1:9800` | gRPC 监听地址（仅本地） |
| `server.shutdown_timeout` | `10s` | 优雅停机超时 |
| `stt.default_model` | `gpt-4o-mini-transcribe` | STT 默认模型 |
| `stt.vad.mode` | `on` | VAD 模式：on / off / debug |
| `stt.vad.hop_size` | `256` | TEN VAD 帧长（256=16ms @ 16kHz） |
| `stt.vad.threshold` | `0.5` | VAD 检测阈值 [0, 1] |
| `tts.default_model` | `gpt-4o-mini-tts` | TTS 默认模型 |
| `tts.default_voice` | `nova` | 默认音色 |
| `tts.default_speed` | `1.22` | 默认语速 |
| `tts.max_sec` | `25.0` | 单段最大时长（超出自动分段） |

**环境变量覆盖**：`PARASPEECH_{SECTION}_{KEY}` 格式，仅非敏感项（含 key/secret/token 的字段被跳过，防止出现在 `/proc/PID/environ`）。

当前版本实际支持的高优先级覆盖项：

- `PARASPEECH_SERVER_GRPC_ADDR`
- `PARASPEECH_STT_VAD_MODE`
- `PARASPEECH_TTS_DEFAULT_VOICE`

配置文件查找顺序：

1. `--config <path>`
2. `PARASPEECH_CONFIG`
3. `/etc/paraspeech/paraspeech.toml`

---

## 可观测性

### 当前已实现

- 结构化日志：`log.format = json|text`，默认 `json`
- 日志级别：`log.level = debug|info|warn|error`
- 日志脱敏：字段名包含 `key/secret/token/sk-` 自动替换为 `[REDACTED]`
- Trace ID：每次 STT/TTS 请求生成 `trace_id`，写入 gRPC 响应元信息（`meta.trace_id`）
- 健康检查：`HealthService.Check` 和 `paraspeech health` 可用于存活与基本配置检查

### 预留但未启用（当前版本）

- `server.metrics_addr` 仅为预留配置
- Prometheus `/metrics` 端点尚未实现
- gRPC 全局拦截器（统一请求日志/指标采集）尚未接入

### 运维建议（当前版本）

1. 用 `journalctl -u paraspeech -f` 观察运行日志
2. 用 `paraspeech health` 或 gRPC `HealthService.Check` 做探活
3. 如需链路排查，优先比对响应中的 `trace_id` 与服务日志时间窗口

---

## gRPC 接口

Proto 定义位于 `api/proto/paraspeech/v1/`。

### STTService

```protobuf
service STTService {
    rpc Transcribe(TranscribeRequest) returns (TranscribeResponse);
    rpc TranscribeStream(stream AudioFrame) returns (stream TranscribeEvent);
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `audio` | bytes | 音频数据（OGG/Opus/WAV/MP3 均可，ffmpeg 自动解码） |
| `filename` | string | 文件名（用于格式推断） |
| `language` | string | 语言提示，可选 |
| `model` | string | 模型覆盖，可选 |
| `vad_debug` | bool | 返回 VAD 元数据 |

响应包含 `text` 和 `TranscribeMeta`（trace_id、audio_ms、process_ms、VadMeta）。

### TTSService

```protobuf
service TTSService {
    rpc Synthesize(SynthesizeRequest) returns (SynthesizeResponse);
    rpc SynthesizeStream(SynthesizeRequest) returns (stream SynthesizeEvent);
    rpc Preview(PreviewRequest) returns (PreviewResponse);
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `text` | string | 待合成文本 |
| `voice_profile` | VoiceProfile | 音色/语速/情感/风格 |
| `model` | string | 模型覆盖 |
| `format` | string | 输出格式：opus, mp3, pcm |
| `max_sec` | double | 单段最大时长覆盖 |

`Preview` RPC 为 dry-run 模式，仅返回分段预览不实际合成。

### HealthService

```protobuf
service HealthService {
    rpc Check(HealthRequest) returns (HealthResponse);
}
```

返回各通道状态（enabled、model、vad_mode、vault_ready）。

`paraspeech health` 为精简输出；如需完整通道字段，建议使用 `grpcurl` 直接调用 `HealthService.Check`。

---

## CLI 使用

CLI 以普通用户运行，所有操作通过 gRPC 委托 serve 进程完成。默认输出 prototext 格式（比 JSON 省约 30% token，对 LLM 友好）。

### 启动服务

```bash
paraspeech serve [--config /etc/paraspeech/paraspeech.toml]
```

### 语音转写

```bash
# 基本用法
paraspeech transcribe /path/to/audio.ogg

# 输出示例（prototext）
# text: "你好，请问有什么可以帮助你的吗？"

# 带 VAD 调试信息
paraspeech transcribe --vad-debug /path/to/audio.ogg

# JSON 格式输出
paraspeech transcribe --format json /path/to/audio.ogg
```

### 语音合成

```bash
# 基本用法
paraspeech synthesize --text "Hello world"

# 完整参数
paraspeech synthesize \
  --text "你好世界" \
  --voice nova \
  --speed 1.22 \
  --emotion cheerful \
  --style conversational \
  --audio-format opus

# 预览分段（不实际合成）
paraspeech synthesize --text "很长的文本..." --dry-run
```

### 健康检查

```bash
paraspeech health

# 输出示例
# ok: true
# service: "paraspeech"
# version: "v1.0.0.0"
```

### 版本

```bash
paraspeech version
```

### CLI Wrapper（兼容旧命令）

```bash
paraspeech-transcribe /path/to/audio.ogg    # 等同 paraspeech transcribe
paraspeech-synthesize --text "..."           # 等同 paraspeech synthesize
```

### grpcurl 调试

```bash
# Health
grpcurl -plaintext \
  -import-path api/proto \
  -proto paraspeech/v1/health.proto \
  -d '{}' \
  127.0.0.1:9800 paraspeech.v1.HealthService/Check

# STT
grpcurl -plaintext \
  -import-path api/proto \
  -proto paraspeech/v1/stt.proto \
  -d '{"audio":"AA==","filename":"sample.wav"}' \
  127.0.0.1:9800 paraspeech.v1.STTService/Transcribe
```

---

## 错误码

| 码 | 名称 | 说明 |
|----|------|------|
| 0 | OK | 成功 |
| 100 | INVALID_REQUEST | 请求参数无效 |
| 101 | EMPTY_INPUT | 空输入 |
| 102 | PAYLOAD_TOO_LARGE | 超过大小限制 |
| 103 | TIMEOUT | 处理超时 |
| 104 | RATE_LIMITED | 限流 |
| 200 | STT_DECODE_FAILED | ffmpeg 解码失败 |
| 201 | STT_VAD_FAILED | VAD 处理失败 |
| 210 | STT_UPSTREAM | 上游 STT 错误 |
| 300 | TTS_SANITIZE_FAILED | 文本清洗失败 |
| 301 | TTS_SPLIT_FAILED | 分段失败 |
| 310 | TTS_UPSTREAM | 上游 TTS 错误 |
| 400 | VAULT_MISSING | 密钥缺失 |

---

## 安全设计

| 威胁 | 防护 |
|------|------|
| OpenClaw 读取密钥文件 | `secrets.env` 权限 `0640 root:paraspeech`，OpenClaw 用户无法 `cat` |
| `/proc/PID/environ` 泄漏 | 密钥不通过环境变量传递 |
| CLI 进程持有密钥 | CLI 纯委托模式，进程内无密钥 |
| panic 堆栈/日志泄漏 | vault 实现 `fmt.Stringer` 返回 `[REDACTED]`；日志 redact key/secret/token 字段 |
| core dump 泄漏 | `LimitCORE=0` + 密钥 mlock 防 swap |
| 密钥残留内存 | 热重载后 memzero 旧密钥 |

---

## TEN VAD

使用 [TEN VAD](https://github.com/TEN-framework/ten-vad) 进行语音活动检测。

- 预编译库放置于 `third_party/ten-vad/`，版本锁定在 `VERSION` 文件
- 通过 CGo 绑定（需 `CGO_ENABLED=1`）
- 当前为 stub 实现，CGo 不可用时自动降级为 `vad.mode=off`
- VAD 回退机制：检测失败/裁剪过度 → 原音频直传上游

---

## License

Internal project.

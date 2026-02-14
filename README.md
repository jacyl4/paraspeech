# ParaSpeech

统一语音中间件 — 将 STT 与 TTS 合并为单一 Go 二进制。

单进程、双通道、gRPC 流式优先、CLI 纯委托、零密钥暴露。

---

## 项目架构

```
              调用方（OpenClaw / 外部服务）
                       │
          ┌────────────┴────────────┐
          │    gRPC :9800           │   CLI 子命令
          │   （流式双工）           │  （委托 gRPC）
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

### 分层结构

| 层 | 职责 | 包 |
|----|------|----|
| L1 Transport | 协议适配 | `transport/grpc`, `transport/cli` |
| L2 Handler | 参数校验、trace 注入 | `transport/grpc/*_handler` |
| L3 Service | 业务编排（decode→VAD→转写 / sanitize→split→合成） | `stt`, `tts` |
| L4 Domain | 核心算法与抽象 | `codec`, `vad`, `voice`, `queue` |
| L5 Infra | 配置、密钥、日志、错误码 | `config`, `vault`, `observe`, `errs` |
| L5 Provider | 上游 API 对接 | `provider/openai`, `provider/deepgram` |

依赖方向：L1 → L2 → L3 → L4/L5，禁止反向引用。

### 运行逻辑

**STT 流水线**：Telegram 语音到达 → 立即启动 ffmpeg pipe 解码（16kHz mono s16le）→ TEN VAD 逐帧检测 → 段合并与裁剪 → OpenAI 批量转写 → 返回文本。前三级（下载/解码/VAD）流式并行。

**TTS 流水线**：文本输入 → markdown/代码/URL 清洗 → 按段落/换行边界分段（保留标点不切，句中不断）→ 逐段调用 OpenAI TTS → 拼接音频返回。

**密钥安全**：`paraspeech serve` 以专用用户运行，密钥文件 `0640 root:paraspeech`。CLI 以普通用户运行，通过 gRPC 委托 serve 完成操作，全程不接触密钥。OpenClaw（LLM agent）无法通过 `cat`/`env`/`/proc` 等任何方式获取密钥。

---

## 目录布局

```
paraspeech/
├── cmd/paraspeech/main.go          # 入口
├── internal/
│   ├── config/                     # TOML 配置 + 环境变量覆盖
│   ├── vault/                      # 密钥金库（SIGHUP 热重载 + mlock + memzero）
│   ├── codec/                      # ffmpeg pipe 封装（零磁盘 IO）
│   ├── vad/                        # TEN VAD 接口 + 段合并算法
│   ├── stt/                        # STT 业务编排
│   ├── tts/                        # TTS 业务编排 + 文本清洗 + 分段
│   ├── voice/                      # VoiceProfile 抽象 + Provider 适配
│   ├── provider/openai/            # OpenAI STT/TTS HTTP 客户端
│   ├── queue/                      # 令牌桶限流
│   ├── transport/grpc/             # gRPC server + handler
│   ├── transport/cli/              # CLI 子命令（纯委托 gRPC）
│   ├── observe/                    # 日志（敏感字段 redact）、trace、metrics
│   └── errs/                       # 统一错误码
├── api/proto/paraspeech/v1/        # Proto 定义（stt, tts, health, common）
├── configs/                        # paraspeech.toml + systemd unit
├── scripts/                        # CLI wrapper（兼容旧命令名）
├── third_party/ten-vad/            # TEN VAD 预编译库 + 版本锁定
├── Makefile
└── ARCHITECTURE.md                 # 完整架构文档（2200+ 行）
```

---

## 构建

**前置依赖**：Go 1.22+、ffmpeg 7.x、make

```bash
# 编译
make build          # → bin/paraspeech

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
sudo install -m 755 bin/paraspeech /usr/local/bin/paraspeech

sudo mkdir -p /etc/paraspeech
sudo cp configs/paraspeech.toml /etc/paraspeech/
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
PARASPEECH_STT_KEY=sk-xxx-stt-dedicated
PARASPEECH_TTS_KEY=sk-xxx-tts-dedicated
```

STT 与 TTS 必须使用不同的 API Key（隔离检查默认开启）。

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

热重载流程：权限预检 → 解析 → 隔离检查 → mlock → 原子替换 → memzero 旧密钥。校验失败时保留旧密钥继续服务。

---

## 配置

主配置文件 `/etc/paraspeech/paraspeech.toml`，完整模板见 `configs/paraspeech.toml`。

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
# version: "0.1.0"
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
| 401 | VAULT_ISOLATION | 密钥隔离检查失败 |

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

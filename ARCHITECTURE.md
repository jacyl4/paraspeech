# ParaSpeech -- 统一语音中间件架构与构建指南

> 将现有 stt-proxy (Python) 和 tts-proxy (Python) 合并为一个 Go 二进制。
> 单进程、双通道、全双工、零密钥暴露。

---

## 目录

1. [为什么重写](#1-为什么重写)
2. [设计原则](#2-设计原则)
3. [总体架构](#3-总体架构)
4. [分层结构](#4-分层结构)
5. [目录布局](#5-目录布局)
6. [核心抽象](#6-核心抽象)
7. [STT 通道详细设计](#7-stt-通道详细设计)
8. [TTS 通道详细设计](#8-tts-通道详细设计)
9. [密钥金库](#9-密钥金库)
10. [可观测性](#10-可观测性)
11. [错误体系](#11-错误体系)
12. [gRPC 接口](#12-grpc-接口)
13. [CLI 接口](#13-cli-接口)
14. [配置体系](#14-配置体系)
15. [部署方案](#15-部署方案)
16. [关键实现片段](#16-关键实现片段)
17. [构建与测试](#17-构建与测试)
18. [迁移路线](#18-迁移路线)
19. [风险与缓解](#19-风险与缓解)
20. [附录：协议定义草案](#20-附录协议定义草案)

---

## 1. 为什么重写

现状的痛点，按严重程度排列：

| 痛点 | 现状 | 目标 |
|------|------|------|
| 双进程冗余 | stt-proxy + tts-proxy 各一个 Python 进程，各自启动/监控/日志 | 单二进制、单进程 |
| 密钥散落 | 两份 `secrets.env`，两套隔离逻辑，手动维护 | 统一金库，一处配置 |
| 错误码各异 | STT 返回 HTTP 状态码+自定义 JSON，TTS 另一套 | 统一 `ErrorCode` 枚举 |
| 日志割裂 | 两个 journald unit，格式不统一 | 结构化 JSON 日志，统一 `trace_id` |
| 无 gRPC | 全靠 HTTP+JSON，无法高效做流式双工 | gRPC 流式优先，CLI 统一入口 |
| 无队列调度 | 并发无限制（Python ThreadingHTTPServer） | 带令牌桶的请求队列 |
| 无指标 | 靠人肉 curl /health | Prometheus metrics 端点 |
| Python 性能 | VAD 用 numpy+ten_vad，GIL 限制并发 | Go + CGo 直接绑定 TEN VAD libten_vad |
| 无优雅停机 | kill 硬杀 | context 传播 + drain |

以下是值得保留的设计：

- 仅监听 127.0.0.1（网络隔离）
- 密钥不出现在请求/响应中
- VAD 回退机制（任何异常 → 原音频直传）
- TTS 文本清洗 + 分段估算
- dry_run 模式
- 先文本后语音的交互顺序

---

## 2. 设计原则

以下每条原则后标注在代码中的映射位置，方便评审时逐条核对。

| 原则 | 在 ParaSpeech 中的体现 |
|------|----------------------|
| **单一职责** | 每个包只做一件事：`stt/` 只管转写，`tts/` 只管合成，`vault/` 只管密钥 |
| **开闭原则** | 上游 provider 通过 `Transcriber` / `Synthesizer` 接口扩展，不改调用方 |
| **里氏替换** | 所有 provider 实现同一接口签名，可互换（OpenAI / Deepgram / Edge） |
| **依赖倒置** | 业务层依赖接口而非具体实现；`server/` 不 import `provider/openai` |
| **接口隔离** | `Transcriber` 只暴露 `Transcribe` 和 `TranscribeStream`；不强迫实现 `Synthesize` |
| **迪米特法则** | handler 只与 service 交互，不直接碰 provider 或 vault 内部 |
| **组合复用** | service 通过字段组合 provider + vault + metrics，不继承 |
| **高内聚低耦合** | `internal/` 下每个子包可独立编译和测试 |

---

## 3. 总体架构

```
                    ┌────────────────────────────────────────────────┐
                    │                  ParaSpeech                    │
                    │              (单二进制 / 单进程)                │
                    ├─────────────────┬──────────────────────────────┤
      调用方        │   gRPC :9800    │   CLI 子命令                 │
     ─────────────► │   (流式双工)     │   (委托 gRPC)               │
                    ├─────────────────┴──────────────────────────────┤
                    │                 路由 / 中间件                    │
                    │   trace_id · 限流 · 超时 · 认证(可选)            │
                    ├───────────────────────────────────────────────-┤
                    │           ┌──────────┐  ┌──────────┐          │
                    │           │ STT Svc  │  │ TTS Svc  │          │
                    │           │ (转写)    │  │ (合成)    │          │
                    │           └────┬─────┘  └────┬─────┘          │
                    │                │              │                │
                    │      ┌─────────┤        ┌────┤                │
                    │      │         │        │    │                │
                    │  ┌───▼───┐ ┌───▼───┐ ┌──▼──┐ │                │
                    │  │  VAD  │ │ Codec │ │Split│ │                │
                    │  │ (预处理)│ │(ffmpeg)│ │(分段)│ │                │
                    │  └───────┘ └───────┘ └─────┘ │                │
                    ├──────────────────────────────-┤                │
                    │          Provider 接口层        │                │
                    │   ┌─────────┐  ┌──────────┐  │                │
                    │   │ OpenAI  │  │ Deepgram │  │  ...可扩展     │
                    │   └────┬────┘  └────┬─────┘  │                │
                    ├────────┼────────────┼────────-┤                │
                    │        │  Vault(密钥金库)│       │                │
                    │        │  隔离 · 轮转 · 审计     │                │
                    ├────────┴────────────┴────────-┤                │
                    │   Observability (日志·指标·追踪)  │                │
                    └───────────────────────────────────────────────-┘
                                      │
                                      ▼
                              上游 API (OpenAI, ...)
```

关键特征：

- **全双工**：gRPC 双向流。STT 入方向持续推送音频帧，出方向持续返回部分文本；TTS 入方向推送文本段，出方向持续返回音频块。
- **纯文本交换**：所有内部服务间通信以结构化文本（protobuf text format / prototext）为载体；二进制音频仅在边界处出现。prototext 既保留 protobuf 的强类型 schema 约束，又具备人类可读性，方便日志审查和手工调试。
- **CLI 纯委托**：`paraspeech transcribe/synthesize` 通过 gRPC 委托 serve 进程完成操作，CLI 本身不持有密钥、不直连上游，安全边界清晰。
- **单进程双通道**：STT 和 TTS 共享进程、共享密钥金库、共享指标收集器，但业务逻辑完全隔离。

---

## 4. 分层结构

从外到内共五层，每层只依赖其内层。

```
┌─────────────────────────────────────────────┐
│ L1  Transport          gRPC / CLI            │
├─────────────────────────────────────────────┤
│ L2  Handler            请求解析 · 响应组装    │
├─────────────────────────────────────────────┤
│ L3  Service            业务编排 · 队列调度    │
├─────────────────────────────────────────────┤
│ L4  Domain             STT/TTS 核心逻辑      │
│     (VAD · Codec · Split · Merge)           │
├─────────────────────────────────────────────┤
│ L5  Infrastructure     Provider · Vault ·    │
│     Metrics · Logger                        │
└─────────────────────────────────────────────┘
```

依赖规则：

- L1 → L2 → L3 → L4 → L5（单向）
- L4 通过接口依赖 L5，不 import 具体实现包
- L1（transport）：gRPC server 走 L2 handler；CLI 通过 gRPC client 委托 serve 进程（CLI 不直连 L3）

---

## 5. 目录布局

```
paraspeech/
├── cmd/
│   └── paraspeech/
│       └── main.go                 # 入口：加载配置、组装依赖、启动服务
│
├── internal/
│   ├── config/
│   │   ├── config.go               # 配置结构定义（TOML 映射）
│   │   └── loader.go               # 配置加载（文件 + 环境变量覆盖）
│   │
│   ├── vault/
│   │   ├── vault.go                # 密钥金库接口 + 实现
│   │   ├── isolation.go            # 密钥隔离验证
│   │   └── vault_test.go
│   │
│   ├── codec/
│   │   ├── ffmpeg.go               # ffmpeg pipe 封装（解码/重采样/编码）
│   │   └── ffmpeg_test.go
│   │
│   ├── vad/
│   │   ├── vad.go                  # VAD 接口定义
│   │   ├── tenvad.go               # TEN VAD 实现（CGo 绑定 libten_vad）
│   │   ├── segment.go              # 语音段合并算法
│   │   ├── fallback.go             # 回退决策逻辑
│   │   └── vad_test.go
│   │
│   ├── stt/
│   │   ├── service.go              # STT 业务编排
│   │   ├── transcriber.go          # Transcriber 接口定义
│   │   └── service_test.go
│   │
│   ├── tts/
│   │   ├── service.go              # TTS 业务编排
│   │   ├── synthesizer.go          # Synthesizer 接口定义
│   │   ├── sanitizer.go            # 文本清洗（去 markdown 等）
│   │   ├── splitter.go             # 文本分段（仅段落/换行切分，保留标点）
│   │   └── service_test.go
│   │
│   ├── voice/
│   │   ├── profile.go              # VoiceProfile 通用结构体定义
│   │   ├── adapter.go              # ProviderAdapter 接口：VoiceProfile → provider 专用参数
│   │   ├── openai.go               # OpenAI TTS adapter（voice → instructions 映射）
│   │   ├── edge.go                 # Edge TTS adapter（预留）
│   │   └── profile_test.go
│   │
│   ├── provider/
│   │   ├── openai/
│   │   │   ├── stt.go              # OpenAI STT 实现
│   │   │   ├── tts.go              # OpenAI TTS 实现
│   │   │   └── client.go           # 共享 HTTP/2 客户端
│   │   ├── deepgram/
│   │   │   └── stt.go              # 预留
│   │   └── edge/
│   │       └── tts.go              # 预留（Edge TTS）
│   │
│   ├── queue/
│   │   ├── limiter.go              # 令牌桶限流器
│   │   └── limiter_test.go
│   │
│   ├── transport/
│   │   ├── grpc/
│   │   │   ├── server.go           # gRPC 服务注册
│   │   │   ├── stt_handler.go      # STT gRPC 处理器
│   │   │   └── tts_handler.go      # TTS gRPC 处理器
│   │   └── cli/
│   │       ├── root.go             # CLI 根命令
│   │       ├── transcribe.go       # paraspeech transcribe <file>
│   │       ├── synthesize.go       # paraspeech synthesize --text "..."
│   │       └── health.go           # paraspeech health
│   │
│   ├── observe/
│   │   ├── logger.go               # 结构化 JSON 日志
│   │   ├── metrics.go              # Prometheus 指标注册
│   │   ├── trace.go                # trace_id 生成与传播
│   │   └── middleware.go           # 通用中间件（日志/指标/trace 注入）
│   │
│   └── errs/
│       ├── codes.go                # 统一错误码枚举
│       └── errors.go               # 错误构造器（带 code + message + details）
│
├── api/
│   └── proto/
│       └── paraspeech/
│           └── v1/
│               ├── stt.proto       # STT gRPC 服务定义
│               ├── tts.proto       # TTS gRPC 服务定义
│               ├── health.proto    # 健康检查
│               └── common.proto    # 共享类型（错误、元数据）
│
├── configs/
│   ├── paraspeech.toml             # 默认配置模板
│   └── paraspeech.service          # systemd unit 模板
│
├── scripts/
│   ├── paraspeech-transcribe       # CLI wrapper（替代 stt-transcribe）
│   └── paraspeech-synthesize       # CLI wrapper（替代 tts_proxy_synth.py）
│
├── third_party/
│   └── ten-vad/
│       ├── VERSION                 # 版本锁定文件
│       ├── include/ten_vad.h       # C API 头文件
│       └── lib/                    # 平台预编译库（见 7.4.1）
│
├── go.mod
├── go.sum
├── Makefile
├── ARCHITECTURE.md                 # 本文件
└── README.md
```

---

## 6. 核心抽象

### 6.1 Transcriber（STT provider 接口）

```go
// internal/stt/transcriber.go

type TranscribeRequest struct {
    Audio       io.Reader
    Filename    string
    Language    string
    Prompt      string
    Format      string  // 期望的响应格式
    Model       string
}

type TranscribeResult struct {
    Text        string
    DurationMS  int64
    Segments    []Segment  // 可选，流式时逐段返回
}

type Segment struct {
    Index    int
    StartMS  int64
    EndMS    int64
    Text     string
}

type Transcriber interface {
    Transcribe(ctx context.Context, req *TranscribeRequest) (*TranscribeResult, error)
    TranscribeStream(ctx context.Context, req *TranscribeRequest, out chan<- *Segment) error
}
```

### 6.2 Synthesizer（TTS provider 接口）

```go
// internal/tts/synthesizer.go

type SynthesizeRequest struct {
    Text         string
    VoiceProfile *voice.VoiceProfile  // 通用声音配置（见 6.3）
    Model        string
    Format       string               // opus, mp3, pcm, ...
}

type SynthesizeResult struct {
    Audio       io.Reader
    ContentType string
    SizeBytes   int64
    DurationMS  int64  // 估算
}

type Synthesizer interface {
    Synthesize(ctx context.Context, req *SynthesizeRequest) (*SynthesizeResult, error)
    SynthesizeStream(ctx context.Context, req *SynthesizeRequest, out chan<- []byte) error
}
```

每个 provider 实现内部持有对应的 `voice.ProviderAdapter`，在调用上游前将 `VoiceProfile` 转换为 provider 专用参数（如 OpenAI 的 `voice` + `instructions` + `speed`）。

### 6.3 VoiceProfile（通用声音控制）

独立模块 `internal/voice/`，与具体 TTS provider 解耦。
所有声音相关参数集中在 VoiceProfile 中，后期更换 API（OpenAI → Azure / ElevenLabs / 自建）时只需新增 adapter，不改调用方。

```go
// internal/voice/profile.go

type VoiceProfile struct {
    Voice    string            // 基础音色标识（如 "nova", "alloy", "shimmer"）
    Speed    float64           // 语速倍率，1.0 为正常
    Emotion  string            // 情感标签（如 "neutral", "cheerful", "serious", "empathetic"）
    Pitch    string            // 音高修饰（如 "default", "low", "high"）
    Style    string            // 风格提示（如 "conversational", "narration", "newscast"）
    Custom   map[string]string // 扩展字段，供特定 provider 使用
}

// ProviderAdapter 将通用 VoiceProfile 转换为特定 provider 的请求参数
type ProviderAdapter interface {
    // MapVoice 返回 provider 专用的音色 ID
    MapVoice(profile *VoiceProfile) string
    // MapInstructions 返回 provider 专用的指令/提示文本（如 OpenAI 的 instructions 字段）
    MapInstructions(profile *VoiceProfile) string
    // MapSpeed 返回 provider 可接受的语速值
    MapSpeed(profile *VoiceProfile) float64
    // SupportsEmotion 该 provider 是否原生支持情感控制
    SupportsEmotion() bool
}
```

```go
// internal/voice/openai.go -- OpenAI TTS adapter 示例

type openAIAdapter struct{}

func (a *openAIAdapter) MapInstructions(p *VoiceProfile) string {
    // OpenAI gpt-4o-mini-tts 支持 instructions 字段控制情感
    // 将 VoiceProfile 的 Emotion/Style 组合为自然语言指令
    if p.Emotion == "" || p.Emotion == "neutral" {
        return ""
    }
    return fmt.Sprintf("Speak in a %s tone with %s style.", p.Emotion, p.Style)
}
```

VoiceProfile 从配置文件或请求参数构建，在 `tts.Service` 层注入到 `SynthesizeRequest`。
各 provider 实现通过对应 adapter 转换后再调上游 API。

### 6.4 Vault（密钥金库接口）

```go
// internal/vault/vault.go

type Vault interface {
    // GetKey 返回指定用途的 API 密钥，不暴露到外部
    GetKey(purpose KeyPurpose) (string, error)
    // Healthy 验证所有密钥已就绪且隔离检查通过
    Healthy() error
}

type KeyPurpose int

const (
    KeySTT KeyPurpose = iota
    KeyTTS
)
```

### 6.5 VAD 接口

基于 TEN VAD（https://github.com/TEN-framework/ten-vad）的 CGo 绑定实现。
TEN VAD 要求输入 **16 kHz mono PCM int16**，支持 hop size 160（10 ms）或 256（16 ms）samples。

```go
// internal/vad/vad.go

// FrameResult 是 TEN VAD 单帧输出
type FrameResult struct {
    Probability float32  // [0.0, 1.0]
    IsVoice     bool
}

type Detector interface {
    // Process 接收一帧 16kHz mono PCM int16（长度 = HopSize），返回该帧 VAD 结果
    Process(frame []int16) (*FrameResult, error)
    // HopSize 返回初始化时设定的帧长（样本数）
    HopSize() int
    Close() error
}

type SegmentMerger interface {
    // Merge 将逐帧 FrameResult 合并为语音段列表
    Merge(results []FrameResult, hopSize int, sampleRate int) []AudioSegment
}

type AudioSegment struct {
    StartSample int
    EndSample   int
    StartMS     int64
    EndMS       int64
}
```

`internal/vad/tenvad.go` 通过 CGo 调用 `libten_vad` 预编译库：

```go
// internal/vad/tenvad.go（核心片段）

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/ten-vad/include
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../../third_party/ten-vad/lib/Linux/x64 -lten_vad
#cgo darwin,arm64 LDFLAGS: -F${SRCDIR}/../../third_party/ten-vad/lib/macOS -framework ten_vad
#include "ten_vad.h"
*/
import "C"

type tenVad struct {
    handle  C.ten_vad_handle_t
    hopSize int
}

func NewTenVad(hopSize int, threshold float32) (Detector, error) {
    var h C.ten_vad_handle_t
    ret := C.ten_vad_create(&h, C.size_t(hopSize), C.float(threshold))
    if ret != 0 {
        return nil, fmt.Errorf("ten_vad_create failed: %d", ret)
    }
    return &tenVad{handle: h, hopSize: hopSize}, nil
}

func (v *tenVad) Process(frame []int16) (*FrameResult, error) {
    var prob C.float
    var flag C.int
    ret := C.ten_vad_process(v.handle,
        (*C.short)(unsafe.Pointer(&frame[0])),
        C.size_t(v.hopSize), &prob, &flag)
    if ret != 0 {
        return nil, fmt.Errorf("ten_vad_process failed: %d", ret)
    }
    return &FrameResult{Probability: float32(prob), IsVoice: flag == 1}, nil
}

func (v *tenVad) Close() error {
    C.ten_vad_destroy(&v.handle)
    return nil
}
```

部署时需将 `libten_vad.so`（Linux）或 `ten_vad.framework`（macOS）放入链接路径，
或通过 `LD_LIBRARY_PATH` 指向 `third_party/ten-vad/lib/Linux/x64/`。

---

## 7. STT 通道详细设计

### 7.1 处理流水线（流式优先）

核心理念：**OpenClaw 收到 Telegram 语音消息的瞬间即启动流式处理，不等待完整文件下载**。
Telegram Bot API 返回的 `file_path` 支持 HTTP Range / chunked 读取，ParaSpeech 边接收边处理。
同一时刻启动“上游连接预热”：在首帧业务数据到达前，先完成 DNS、TCP、TLS、HTTP/2 握手，后续请求仅走 HTTP/2 连接复用，避免每次流式任务重复握手带来的首包延迟。

连接预热失败时采用 **fail-open 降级**，保证业务不中断：

1. 预热失败不阻塞任务主流程，立即进入“首请求建连”路径继续处理。
2. 首请求若建连成功，本任务继续；同时异步回填连接池，供后续任务复用。
3. 首请求建连若超时/失败，按现有上游错误策略返回 `ErrSTTUpstreamTimeout` / `ErrSTTUpstream`（TTS 同理）。
4. 预热失败仅记录 `warn` 日志和指标，不提升为进程级 `ready` 失败。

```
Telegram voice message 到达 OpenClaw
  │
  ├─ 0. 立即启动 TranscribeStream RPC
  │     OpenClaw → ParaSpeech gRPC 双向流
  │     OpenClaw 一边从 Telegram 下载，一边向 ParaSpeech 推送 AudioFrame
  │
  ├─ 1. 大小检查（MaxBytes 上限，边接收边累加）
  │
  ├─ 2. 流式解码
  │     ffmpeg pipe（stdin ← 流式写入 OGG/Opus 数据）
  │     stdout → 16kHz mono PCM int16
  │     零磁盘 IO，纯内存管道
  │     每读出一帧 PCM 即送入下一步
  │
  ├─ 3. 逐帧 TEN VAD
  │     每积攒 hopSize（256 samples = 16ms）PCM：
  │       detector.Process(frame) → FrameResult{Probability, IsVoice}
  │     结果实时追加到 FrameResult 滑动窗口
  │     ├─ mode=off → 跳过 VAD，PCM 帧直接送步骤 4
  │     ├─ SegmentMerger 实时合并 → 检测到语音段结束时输出 AudioSegment
  │     └─ 安全检查：trimRatio < MinTrimRatio → 回退原音频
  │
  ├─ 4. 上游转写
  │     ├─ 当前 OpenAI 上游（gpt-4o-mini-transcribe）：批量提交
  │     │   VAD 裁剪完成后，将有效音频一次性 multipart POST 上传
  │     │   前三级（下载‖解码‖VAD）仍然并行，总等待 = max(下载, 解码+VAD) + 上游转写
  │     └─ 未来流式上游（Deepgram 等）：TranscribeStream 逐帧推送
  │         上游逐步返回 partial text → 四级全并行
  │
  └─ 5. 结束
        ffmpeg 收到 EOF → VAD flush → 上游提交/EndOfAudio → FinalResult
        返回完整文本 + VadMeta + TranscribeMeta
```

**上游约束说明**：OpenAI `/v1/audio/transcriptions` 当前仅支持一次性提交完整音频文件（multipart POST），不支持流式输入。因此现阶段 `Transcriber.TranscribeStream` 接口在 OpenAI provider 内部实现为"攒帧→一次性提交"，对外接口不变。待接入 Deepgram 等支持 WebSocket 流式输入的 STT 后，同一接口可实现真正的四级全并行。

**ffmpeg 流式 pipe 可行性**：现有 Python stt-proxy 已验证 `ffmpeg -i pipe: -ac 1 -ar 16000 -f wav pipe:1` 对 OGG/Opus 的 pipe 解码可靠工作。Go 端复用同一 ffmpeg 命令行，通过 `os/exec` 管道对接。

**时序对比**（以 5 秒语音为例）：

| 策略 | 总延迟 |
|------|--------|
| 旧：下载完 → 解码 → VAD → 转写（全串行） | 下载 0.8s + 解码 0.3s + VAD 0.1s + 转写 2.0s ≈ **3.2s** |
| 新（OpenAI 批量上游）：下载 ‖ 解码 ‖ VAD → 转写 | max(0.8, 0.3+0.1) + 转写 2.0s ≈ **2.8s**（VAD 裁剪后上传体积更小，转写可能更快） |
| 未来（流式上游）：下载 ‖ 解码 ‖ VAD ‖ 转写 | 下载 0.8s + 流水线重叠 ≈ **1.5s** |

### 7.2 流式转写（gRPC 双向流 + Telegram 即时触发）

```
Telegram          OpenClaw                  ParaSpeech                  Upstream
  │                  │                          │                          │
  │─ voice msg ────► │                          │                          │
  │                  │── TranscribeStream() ───► │                          │
  │                  │                          │                          │
  │◄─ download ─────►│── AudioFrame(chunk1) ──► │─ ffmpeg ─► PCM           │
  │  (chunked)       │── AudioFrame(chunk2) ──► │─ TEN VAD ─► 语音帧 ───► │
  │                  │                          │                          │
  │                  │                          │◄── partial text ───────  │
  │                  │◄── PartialResult ──────  │                          │
  │                  │  (可实时推送到聊天窗口)     │                          │
  │                  │                          │                          │
  │◄─ download ─────►│── AudioFrame(chunk3) ──► │─ VAD + 转发有效帧 ─────► │
  │  (继续)          │── EndOfAudio ──────────► │─ flush ──────────────► │
  │                  │                          │◄── final text ────────  │
  │                  │◄── FinalResult ────────  │                          │
```

关键点：
- OpenClaw **不等 Telegram 文件下载完毕**，收到 `voice` 消息即刻建立 gRPC 流
- 下载与解码/VAD/转写四阶段并行（pipeline 模式），首个 PartialResult 延迟显著降低
- TEN VAD 每 16 ms 一帧判定，静音帧不推向上游，节省转写配额

### 7.3 VAD 实现选择

| 方案 | 优点 | 缺点 | 建议 |
|------|------|------|------|
| **TEN VAD（CGo 预编译库）** | 精度优于 Silero；帧粒度更细（10/16 ms vs 32 ms）；官方提供 Go CGo 绑定和预编译 .so/.framework；Apache-2.0 | CGo 依赖，需部署 `libten_vad` | **首选** |
| Silero VAD (ONNX) | Go 社区有 onnxruntime 绑定 | 需额外 ONNX Runtime 动态库；帧粗（32 ms） | 备选 |
| WebRTC VAD (Go 纯实现) | 无 CGo | 精度最低 | 不推荐 |

**选定 TEN VAD**（https://github.com/TEN-framework/ten-vad）。关键参数：

- **输入**：16 kHz mono PCM int16
- **Hop size**：256 samples（16 ms），配置项 `stt.vad.hop_size`
- **Threshold**：0.5（可调），配置项 `stt.vad.threshold`
- **输出**：每帧返回 `(probability float32, isVoice bool)`
- **Go API**：`NewVad(hopSize, threshold)` → `vad.Process([]int16)` → `vad.Close()`
- **预编译库**：`lib/Linux/x64/libten_vad.so`、`lib/macOS/ten_vad.framework`
- **运行依赖**：Linux 需 `libc++1`（`apt install libc++1`）

### 7.4 TEN VAD 集成与维护

#### 7.4.1 预编译库管理

TEN VAD 不以 Go module 形式分发，而是提供平台相关的预编译共享库 + C 头文件。
ParaSpeech 将其纳入 `third_party/` 目录统一管理：

```
paraspeech/
└── third_party/
    └── ten-vad/
        ├── VERSION                     # 当前集成版本号，如 "1.0-ONNX"
        ├── LICENSE                     # Apache-2.0
        ├── include/
        │   └── ten_vad.h              # C API 头文件
        └── lib/
            ├── Linux/
            │   └── x64/
            │       └── libten_vad.so  # Linux amd64 预编译库
            └── macOS/
                └── ten_vad.framework/ # macOS universal framework
```

#### 7.4.2 版本锁定与升级流程

1. **版本锁定**：`third_party/ten-vad/VERSION` 文件记录当前使用的 release tag（如 `v1.0-ONNX`）。CI 构建时校验此文件与实际库版本一致。
2. **升级步骤**：
   ```bash
   # 1. 从 GitHub Release 或 HuggingFace 下载新版
   wget https://github.com/TEN-framework/ten-vad/releases/download/<tag>/...
   # 2. 替换 third_party/ten-vad/lib/ 下对应平台文件
   # 3. 更新 VERSION 文件
   echo "<new-tag>" > third_party/ten-vad/VERSION
   # 4. 运行对比测试
   go test ./internal/vad/... -run TestTenVadAccuracy -count=1
   # 5. 逐样本比较新旧版本的 probability 输出偏差
   ```
3. **回退**：git checkout 恢复 `third_party/ten-vad/` 即可立即回退到旧版本。
4. **ONNX 替代路径**：TEN VAD 已开源 ONNX 模型（`src/` 目录）。如未来需要脱离预编译库，可改用 `onnxruntime-go` 加载 ONNX 模型，接口层（`Detector`）不变。

#### 7.4.3 CGo 构建注意事项

- **CGo 编译标志**：定义在 `internal/vad/tenvad.go` 头部的 `#cgo` 指令中，路径相对于源文件位置。
- **交叉编译**：CGo 限制了 `GOOS/GOARCH` 交叉编译。如需为不同平台构建，需在对应平台上编译或使用 Docker（如 `golang:1.23-bookworm`）。
- **运行时依赖**：部署机器需要：
  - Linux：`libc++1`（`apt install libc++1`），`libten_vad.so` 在 `LD_LIBRARY_PATH` 中
  - macOS：`ten_vad.framework` 在 `DYLD_FRAMEWORK_PATH` 中
- **禁用 CGo 的构建模式**：设置 `CGO_ENABLED=0` 时 VAD 功能自动降级，`NewTenVad()` 返回 `ErrVadNotAvailable`，服务自动回退到 `vad.mode=off`。

#### 7.4.4 测试基准

`internal/vad/vad_test.go` 包含：

| 测试 | 目的 |
|------|------|
| `TestTenVadCreate` | 确认 CGo 绑定可正常初始化和销毁 |
| `TestTenVadSilence` | 全零帧应返回 `IsVoice=false` |
| `TestTenVadSpeech` | 已知语音 WAV 的帧应返回 `IsVoice=true`（概率 > threshold） |
| `TestTenVadAccuracy` | 与 Python `ten_vad` 处理同一 WAV，逐帧比对 probability 偏差 < 1e-6 |
| `BenchmarkTenVadProcess` | 单帧处理延迟基准（目标 < 0.1 ms/帧） |

### 7.5 语音段合并算法（移植）

现有 Python `merge_voice_segments()` 完整移植到 `internal/vad/segment.go`：

1. 遍历 voice flags → 连续 true 区间
2. 过滤短于 `MinSpeechMS` 的段
3. 间隙 <= `MaxGapMS` 的相邻段合并
4. 连续静音 >= `MinSilenceMS` 才算断开
5. 每段前后扩展 `PadMS`
6. 重叠消除

所有阈值从 `config.VAD` 读取，与现有环境变量名一一对应。

---

## 8. TTS 通道详细设计

### 8.1 处理流水线

```
文本输入 (string)
  │
  ├─ 1. 文本清洗 sanitizer.Clean(text)
  │     去 markdown / 代码块 / URL / 控制标签
  │     与现有 _dehydrate_text() 逻辑一致
  │
  ├─ 2. 构建 VoiceProfile
  │     从请求参数 + 配置默认值 → voice.VoiceProfile{Voice, Speed, Emotion, ...}
  │     传入 provider adapter 生成上游专用参数
  │
  ├─ 3. 文本分段 splitter.Split(text, maxSec)
  │     **保留标点不切**：标点符号影响 TTS 的停顿、语调和情感表达，
  │     不在句中标点（逗号、分号、冒号等）处切分。
  │     分段策略（优先级从高到低）：
  │       a. 段落边界（\n\n）
  │       b. 换行符（\n）
  │       c. 仅当单段估算时长超过 maxSec 时，才在句末标点（。！？.!?）后切
  │       d. 绝不在逗号、分号、感叹号内部等句中标点处切
  │     时长估算：CJK 4.2字/s, Latin 2.6词/s, 安全系数 0.88
  │     每段保留其结尾标点，确保情绪表达完整
  │
  ├─ 4. dry_run → 仅返回分段预览
  │
  ├─ 5. 逐段合成
  │     Synthesizer.Synthesize(ctx, req{VoiceProfile}) → result
  │     或 Synthesizer.SynthesizeStream → chan []byte
  │
  ├─ 6. 音频落盘（可选，按配置）
  │     path: {outDir}/{session}/{timestamp}/seg-{nn}.{fmt}
  │
  └─ 7. 返回段列表 + 元数据
```

**分段策略设计原则**：TTS 上游（如 OpenAI gpt-4o-mini-tts）依赖标点符号生成正确的韵律。
感叹号 → 激昂语调，问号 → 上扬语调，省略号 → 延长停顿。
如果在标点处硬切，后续片段丢失上下文，情感表达会断裂。
因此采用"尽量不切"策略，仅在段落边界自然切分。

### 8.2 流式合成（gRPC 服务端流）

```
Client                          ParaSpeech                    Upstream
  │                                │                            │
  │── SynthesizeRequest ─────────► │                            │
  │   (text, voice, speed, ...)    │─ 分段 ──────────────────► │
  │                                │                            │
  │                                │◄── audio chunk 1 ───────  │
  │◄── AudioChunk(seg=1,data) ──  │                            │
  │                                │◄── audio chunk 2 ───────  │
  │◄── AudioChunk(seg=1,data) ──  │                            │
  │                                │── 下一段 ──────────────► │
  │◄── AudioChunk(seg=2,data) ──  │                            │
  │◄── FinalMeta ────────────────  │                            │
```

### 8.3 失败策略

与现有一致，增强为结构化错误：

- 任何段合成失败 → 返回 `ErrorCode_TTS_UPSTREAM_FAILED`
- 响应中携带 `fallback_marker`
- 已完成的段信息仍然返回（改进：不再中止已完成段的传输）

---

## 9. 密钥金库

### 9.1 威胁模型

ParaSpeech 的主要调用方是 OpenClaw —— 一个 LLM agent。
LLM agent 的本质是：**它会阅读并理解所有能触及的文本**。
这意味着传统"不在响应中返回密钥"是必要但不充分的。需要假设 OpenClaw 有能力：

| 攻击面 | 风险 | 说明 |
|--------|------|------|
| CLI stdout / stderr | 高 | OpenClaw 会完整消费命令输出，任何泄漏到输出的密钥都会进入 LLM 上下文 |
| 环境变量 | 高 | 若密钥通过 `PARASPEECH_STT_KEY=sk-xxx` 传入，同用户的 `cat /proc/<pid>/environ` 可读 |
| 配置文件可读 | 高 | 若 OpenClaw 进程能 `cat /etc/paraspeech/secrets.env`，密钥直接暴露 |
| 错误堆栈 / panic | 中 | Go panic 可能打印内存中的字符串，含密钥片段 |
| core dump | 中 | 进程崩溃时 core 文件含内存快照 |
| gRPC 元数据 | 低 | 内部 127.0.0.1，但仍不应携带密钥 |

**设计目标**：即使 OpenClaw 被 prompt injection 诱导去执行 `cat`、`env`、`strings /proc/*/mem` 等命令，也无法获取上游 API 密钥。

### 9.2 架构：serve 常驻 + CLI 纯委托

核心原则：**密钥只存在于 serve 进程内存中，CLI 进程永远不接触密钥**。

```
┌─────────────┐     gRPC 127.0.0.1:9800     ┌──────────────────┐
│  CLI 进程    │ ──────────────────────────► │  serve 进程       │
│  user:jacyl4 │   只传音频/文本，不传密钥     │  user:paraspeech  │
│  无密钥      │ ◄────────────────────────── │  持有密钥（内存） │
│             │   只返回转写/合成结果          │                  │
└─────────────┘                              └───────┬──────────┘
      │                                              │ 启动/SIGHUP 时读取
      │ 不可读                                        ▼
      ╳──────────────────────────────────    /etc/paraspeech/secrets.env
                                             owner: root:paraspeech
                                             mode: 0640
```

```
用户/进程          能读取的内容                    不能读取的内容
─────────────────────────────────────────────────────────────────
OpenClaw           paraspeech CLI 的 stdout/stderr  secrets.env
(user: jacyl4)     paraspeech.toml（不含密钥）       /proc/<paraspeech>/environ
                   音频文件、文本结果                 paraspeech 进程内存

paraspeech serve   secrets.env（读取后丢弃 fd）      N/A（自身持有）
(user: paraspeech) paraspeech.toml

paraspeech CLI     gRPC 响应（转写/合成结果）        secrets.env
(user: jacyl4)     paraspeech.toml                   密钥（完全不经手）
```

- `secrets.env` 属主 `root:paraspeech`，权限 `0640`
- `paraspeech` 用户属于 `paraspeech` 组，可读
- `jacyl4`（OpenClaw 运行用户）**不在** `paraspeech` 组，无法读取
- CLI 进程以 `jacyl4` 身份运行，通过 gRPC 委托 serve 进程完成所有需要密钥的操作
- CLI 进程本身**永远不接触密钥**，也不需要 setgid 等特殊权限
- **serve 不可达时 CLI 直接报错退出**，不尝试独立加载密钥（减少攻击面和复杂度）

CLI 和 serve 使用**同一份配置文件**，查找顺序：

```
1. --config <path>                          # 命令行显式指定
2. $PARASPEECH_CONFIG                       # 环境变量
3. /etc/paraspeech/paraspeech.toml          # 系统级默认路径
```

配置文件（`paraspeech.toml`）**不含密钥**，仅指向密钥文件路径：

```toml
[vault]
secrets_file = "/etc/paraspeech/secrets.env"   # 密钥文件路径（文件本身受权限保护）
enforce_isolation = true
```

配置文件权限 `0644`（所有用户可读），因为其中无敏感信息。

### 9.3 实现

```go
type fileVault struct {
    keys     map[KeyPurpose]string  // 内存中，不可序列化
    isolated bool
    loaded   bool
}
```

### 9.4 加载流程（纵深防御）

启动时和 SIGHUP 热重载时共用同一条加载路径：

```
loadSecrets()（启动 / SIGHUP 触发）
  │
  ├─ 1. 读取 secrets_file 路径（从 TOML 配置）
  │
  ├─ 2. 权限预检
  │     ├─ 检查文件 mode：必须 <= 0640（拒绝 world-readable）
  │     ├─ 检查文件 owner group：必须为 paraspeech 组
  │     └─ 不通过 → 启动时：进程退出；SIGHUP 时：保留旧密钥，记录 error 日志
  │
  ├─ 3. 读取 + 解析
  │     ├─ 打开文件 → 读取全部内容到 []byte → 立即关闭 fd
  │     ├─ 解析 KEY=VALUE → 构建 newKeys map[KeyPurpose]string
  │     └─ 用 mlock(2) 锁定新密钥内存页，防止 swap 到磁盘
  │
  ├─ 4. 隔离检查
  │     ├─ STT key != TTS key（强制使用独立密钥）
  │     └─ 不通过 → 启动时：进程退出；SIGHUP 时：保留旧密钥，记录 error 日志
  │
  ├─ 5. 原子替换
  │     ├─ mu.Lock()
  │     ├─ memzero(oldKeys)     // 覆写旧密钥
  │     ├─ vault.keys = newKeys // 原子切换
  │     └─ mu.Unlock()
  │
  └─ 6. 日志记录（不打印密钥内容）
        ├─ 启动时："vault loaded, stt_key=sk-***…***<last4>, tts_key=sk-***…***<last4>"
        └─ SIGHUP："vault reloaded, keys rotated"
```

### 9.5 密钥轮换操作流程

日常更换密钥只需两步：

```bash
# 1. root 用户编辑密钥文件
sudo vim /etc/paraspeech/secrets.env

# 2a. 热重载（推荐，零停机）
sudo kill -HUP $(pidof paraspeech)
# 或
sudo systemctl reload paraspeech    # 需配置 ExecReload（见下方）

# 2b. 冷重启（备选）
sudo systemctl restart paraspeech
```

SIGHUP 热重载的安全保证：

| 场景 | 行为 |
|------|------|
| 新密钥文件格式正确、权限正确 | 原子替换内存中的密钥，旧密钥 memzero 覆写，零停机 |
| 新密钥文件权限不对（如被改为 0644） | **拒绝加载**，保留旧密钥继续服务，记录 `error` 日志 |
| 新密钥文件不存在或不可读 | **拒绝加载**，保留旧密钥继续服务，记录 `error` 日志 |
| 新密钥隔离检查失败（STT key == TTS key） | **拒绝加载**，保留旧密钥继续服务，记录 `error` 日志 |
| SIGHUP 期间有请求正在处理 | 不影响，在途请求用旧密钥完成，后续请求用新密钥 |

原则：**SIGHUP 永远不会让服务变得比重载前更差**。任何校验失败都保留旧状态。

### 9.6 安全规则（完整清单）

| 层级 | 规则 | 实现 |
|------|------|------|
| **文件** | 密钥文件不可被 OpenClaw 用户读取 | `secrets.env` 权限 `0640 root:paraspeech`；启动时校验 |
| **文件** | 配置文件不含密钥 | `paraspeech.toml` 只存密钥文件路径，权限 `0644` |
| **环境** | 密钥不通过环境变量传递 | 不支持 `PARASPEECH_STT_KEY` 环境变量覆盖密钥；仅非敏感配置可环境变量覆盖 |
| **进程** | 密钥不出现在 /proc/PID/environ | 因为密钥从文件读取，不通过 env 传入 |
| **进程** | 密钥不出现在 /proc/PID/cmdline | 密钥不作为命令行参数 |
| **内存** | 密钥页锁定防 swap | `mlock(2)` 锁定密钥 map 所在内存页 |
| **内存** | 退出时覆写 | shutdown hook 中 `memzero` 覆写密钥字节 |
| **输出** | 密钥不出现在 stdout/stderr | handler/CLI 层永远不序列化 vault 内容 |
| **输出** | 密钥不出现在日志 | logger 自动 redact 含 `key`/`secret`/`token`/`sk-` 的字段 |
| **输出** | 密钥不出现在 panic 堆栈 | vault 结构体实现 `fmt.Stringer` 返回 `[REDACTED]` |
| **输出** | 密钥不出现在 Prometheus 指标 | metrics label 不含密钥相关值 |
| **网络** | gRPC 元数据不携带密钥 | 密钥仅在 provider 层使用，不经过 gRPC metadata |
| **崩溃** | core dump 禁用 | systemd `LimitCORE=0`；Go `GOTRACEBACK=crash` 不含堆内存 |
| **隔离** | STT / TTS 使用独立密钥 | 启动时强制校验 `enforce_isolation` |

### 9.7 CLI 密钥隔离

CLI 进程**完全不接触密钥**，架构上杜绝泄漏可能：

```
CLI 命令执行（user: jacyl4）
  │
  ├─ 连接 serve 进程（gRPC 127.0.0.1:9800）
  │   ├─ 成功 → 委托 serve 完成操作 → 返回结果
  │   └─ 失败 → 立即报错退出（stderr 输出连接失败信息）
  │             不尝试自行加载密钥，不降级
  │
  └─ CLI 进程全程无密钥，无特殊权限
```

如果 serve 不可达，CLI 输出明确错误提示：

```prototext
error {
  code: 199
  name: "INTERNAL"
  message: "paraspeech serve 未运行，请先启动：sudo systemctl start paraspeech"
}
```

这种"不降级"设计意味着 **paraspeech serve 必须运行**才能使用 CLI。
好处是安全模型极简：密钥只在 serve 进程（user:paraspeech）中，一个隔离边界，没有例外。

---

## 10. 可观测性

### 10.1 结构化日志

所有日志条目为 JSON，统一字段：

```json
{
  "ts": "2026-02-14T10:00:00.123Z",
  "level": "info",
  "msg": "transcribe_complete",
  "trace_id": "abc123",
  "channel": "stt",
  "duration_ms": 1180,
  "audio_ms": 3960,
  "vad": {
    "enabled": true,
    "trimmed": true,
    "ratio": 0.59
  },
  "upstream": {
    "model": "gpt-4o-mini-transcribe",
    "status": 200,
    "latency_ms": 980
  }
}
```

日志级别：

| 级别 | 用途 |
|------|------|
| `error` | 上游失败、密钥缺失、不可恢复错误 |
| `warn` | VAD 回退、超时重试、限流触发 |
| `info` | 请求完成、启动/停机 |
| `debug` | VAD 帧级细节、分段详情、请求/响应体摘要 |

### 10.2 Prometheus 指标

```
paraspeech_requests_total{channel="stt|tts", status="ok|error"}
paraspeech_request_duration_seconds{channel="stt|tts"}
paraspeech_upstream_duration_seconds{channel="stt|tts", provider="openai"}
paraspeech_audio_duration_seconds{channel="stt"}          # 输入音频时长
paraspeech_vad_trim_ratio{channel="stt"}                   # histogram
paraspeech_vad_fallback_total{channel="stt", reason="..."}
paraspeech_tts_segments_total{channel="tts"}
paraspeech_tts_segment_estimated_seconds{channel="tts"}
paraspeech_active_requests{channel="stt|tts"}              # gauge
paraspeech_queue_depth{channel="stt|tts"}                  # gauge
paraspeech_vault_healthy{purpose="stt|tts"}                # gauge 0|1
```

### 10.3 trace_id 传播

- 每个入站请求生成 `trace_id`（UUID v7，含时间戳）
- gRPC：metadata `x-trace-id`
- CLI：写入 stderr
- 所有日志条目、指标 label、错误响应均携带 `trace_id`

### 10.4 健康检查

```
gRPC Health/Check      → 标准 gRPC 健康协议
CLI `paraspeech health` → 本地健康检查（默认 prototext，可 --format json）
```

---

## 11. 错误体系

### 11.1 错误码枚举

统一整数错误码，STT 和 TTS 共享同一命名空间：

```go
// internal/errs/codes.go

type Code int

const (
    OK                 Code = 0

    // 通用 1xx
    ErrInvalidRequest  Code = 100  // 请求格式错误
    ErrEmptyInput      Code = 101  // 空音频或空文本
    ErrPayloadTooLarge Code = 102  // 超过大小限制
    ErrTimeout         Code = 103  // 处理超时
    ErrRateLimited     Code = 104  // 限流
    ErrInternal        Code = 199  // 未分类内部错误

    // STT 2xx
    ErrSTTDecodeFailed Code = 200  // 音频解码失败
    ErrSTTVadFailed    Code = 201  // VAD 处理失败（已回退）
    ErrSTTUpstream     Code = 210  // 上游转写失败
    ErrSTTUpstreamTimeout Code = 211

    // TTS 3xx
    ErrTTSSanitizeFailed Code = 300  // 文本清洗异常
    ErrTTSSplitFailed    Code = 301  // 分段失败
    ErrTTSUpstream       Code = 310  // 上游合成失败
    ErrTTSUpstreamTimeout Code = 311

    // 密钥 4xx
    ErrVaultMissing    Code = 400  // 密钥缺失
    ErrVaultIsolation  Code = 401  // 密钥隔离违规
)
```

### 11.2 错误响应格式

gRPC 与 CLI 共享同一错误语义：

- **gRPC**：通过 `google.rpc.Status` + `details` 传递错误码和元信息
- **CLI**：prototext 格式写入 stderr

```prototext
error {
  code: 210
  name: "STT_UPSTREAM"
  message: "upstream returned HTTP 429"
  trace_id: "abc123"
  details { upstream_status: 429 }
}
```

### 11.3 错误到 gRPC status 映射

| 错误码范围 | gRPC status |
|-----------|-------------|
| 100-102 | `InvalidArgument` |
| 103 | `DeadlineExceeded` |
| 104 | `ResourceExhausted` |
| 199 | `Internal` |
| 200-201 | `FailedPrecondition` |
| 210-211 | `Unavailable` |
| 300-301 | `InvalidArgument` |
| 310-311 | `Unavailable` |
| 400-401 | `FailedPrecondition` |

---

## 12. gRPC 接口

### 12.1 STT 服务

```protobuf
// api/proto/paraspeech/v1/stt.proto

service STTService {
    // 单次转写：发送完整音频，返回完整文本
    rpc Transcribe(TranscribeRequest) returns (TranscribeResponse);

    // 流式转写：双向流
    rpc TranscribeStream(stream AudioFrame) returns (stream TranscribeEvent);
}

message TranscribeRequest {
    bytes audio = 1;
    string filename = 2;
    string language = 3;
    string model = 4;
    bool vad_debug = 5;
}

message TranscribeResponse {
    string text = 1;
    TranscribeMeta meta = 2;
}

message TranscribeMeta {
    string trace_id = 1;
    int64 audio_ms = 2;
    int64 process_ms = 3;
    VadMeta vad = 4;
}

message VadMeta {
    bool enabled = 1;
    string reason = 2;
    bool fallback = 3;
    int64 audio_ms_before = 4;
    int64 audio_ms_after = 5;
    double trim_ratio = 6;
    int32 segments_count = 7;
    int64 elapsed_ms = 8;
}

message AudioFrame {
    bytes data = 1;
    bool end_of_audio = 2;
}

message TranscribeEvent {
    oneof event {
        PartialResult partial = 1;
        FinalResult final = 2;
        TranscribeError error = 3;
    }
}

message PartialResult {
    int32 index = 1;
    double start_sec = 2;
    double end_sec = 3;
    string text = 4;
    string accumulated_text = 5;
}

message FinalResult {
    string text = 1;
    int32 chunks = 2;
    TranscribeMeta meta = 3;
}
```

### 12.2 TTS 服务

```protobuf
// api/proto/paraspeech/v1/tts.proto

service TTSService {
    // 单次合成：返回完整音频
    rpc Synthesize(SynthesizeRequest) returns (SynthesizeResponse);

    // 流式合成：服务端流，逐段/逐块返回音频
    rpc SynthesizeStream(SynthesizeRequest) returns (stream SynthesizeEvent);

    // 预览分段（dry run）
    rpc Preview(PreviewRequest) returns (PreviewResponse);
}

message SynthesizeRequest {
    string text = 1;
    string session = 2;
    VoiceProfile voice_profile = 3;
    string model = 4;
    string format = 5;       // opus, mp3, pcm
    double max_sec = 6;
    string out_dir = 7;      // 可选，服务端落盘路径
}

message VoiceProfile {
    string voice = 1;        // 基础音色（nova, alloy, shimmer, ...）
    double speed = 2;        // 语速倍率
    string emotion = 3;      // 情感标签（neutral, cheerful, serious, ...）
    string pitch = 4;        // 音高（default, low, high）
    string style = 5;        // 风格（conversational, narration, ...）
    map<string, string> custom = 6;  // 扩展字段
}

message SynthesizeResponse {
    int32 count = 1;
    repeated SynthesizeSegment segments = 2;
    SynthesizeMeta meta = 3;
}

message SynthesizeSegment {
    int32 index = 1;
    string text = 2;
    double estimated_sec = 3;
    bytes audio = 4;           // 单次模式时填充
    string path = 5;           // 落盘路径（如果启用）
    int64 size_bytes = 6;
    string content_type = 7;
}

message SynthesizeMeta {
    string trace_id = 1;
    VoiceProfile voice_profile = 2;
    string model = 3;
    double max_sec = 4;
    int64 process_ms = 5;
}

message SynthesizeEvent {
    oneof event {
        AudioChunk chunk = 1;
        SegmentDone segment_done = 2;
        SynthesizeMeta final_meta = 3;
        SynthesizeError error = 4;
    }
}

message AudioChunk {
    int32 segment_index = 1;
    bytes data = 2;
    string content_type = 3;
}

message SegmentDone {
    int32 index = 1;
    string text = 2;
    double estimated_sec = 3;
    int64 size_bytes = 4;
    string path = 5;
}

message PreviewRequest {
    string text = 1;
    double max_sec = 2;
}

message PreviewResponse {
    int32 count = 1;
    repeated PreviewSegment segments = 2;
}

message PreviewSegment {
    int32 index = 1;
    string text = 2;
    double estimated_sec = 3;
}
```

---

## 13. CLI 接口

### 13.1 设计原则：省 token

ParaSpeech CLI 的主要调用方是 OpenClaw（LLM agent），其输出会被 LLM 消费。
JSON 的引号、花括号、冒号等语法字符对 LLM token 计数不友好。
因此 CLI **默认输出 prototext 格式**，与内部 protobuf schema 统一，同时比 JSON 省约 30% token。

### 13.2 命令一览

```bash
# 服务模式
paraspeech serve [--config /etc/paraspeech/paraspeech.toml]

# 转写
paraspeech transcribe /path/to/audio.ogg
paraspeech transcribe --stream /path/to/long-audio.ogg

# 合成
paraspeech synthesize --text "Hello world"
paraspeech synthesize --text "..." --voice nova --speed 1.22 --emotion cheerful --format opus
paraspeech synthesize --text "..." --dry-run

# 健康检查
paraspeech health

# 版本
paraspeech version
```

### 13.3 输出格式

**默认 prototext**（stdout），元数据和错误写 stderr。

转写正常输出：
```prototext
text: "你好，请问有什么可以帮助你的吗？"
```

转写 debug 模式（`--vad-debug`）：
```prototext
text: "你好"
meta {
  trace_id: "abc123"
  audio_ms: 5200
  process_ms: 1180
  vad {
    enabled: true
    fallback: false
    audio_ms_before: 5200
    audio_ms_after: 3100
    trim_ratio: 0.5962
    segments_count: 2
    elapsed_ms: 45
  }
}
```

合成输出：
```prototext
count: 1
segments {
  index: 1
  text: "你好"
  estimated_sec: 0.48
  path: "/home/jacyl4/.openclaw/media/outbound/paraspeech/telegram-main/20260215-100000/seg-01.opus"
  size_bytes: 4096
  content_type: "audio/opus"
}
```

错误输出（stderr）：
```prototext
error {
  code: 310
  name: "TTS_UPSTREAM"
  message: "upstream returned HTTP 429"
  trace_id: "abc123"
}
```

可选 `--format json` 切换为 JSON 输出（供调试或第三方集成）。

### 13.4 Token 节省对比

以转写响应为例：

| 格式 | 示例 | 约 token 数 |
|------|------|------------|
| JSON | `{"text":"你好","meta":{"trace_id":"abc123","process_ms":1180}}` | ~28 |
| prototext | `text: "你好"\nmeta { trace_id: "abc123" process_ms: 1180 }` | ~20 |
| 纯文本（正常模式） | `text: "你好"` | ~5 |

OpenClaw 日常调用只取 `text` 字段，正常模式下仅输出一行，极致省 token。

### 13.5 运行通路与密钥安全

CLI 纯委托，**不接触密钥**（详见 [第 9 节 密钥金库](#9-密钥金库)）：

```
CLI 进程启动（user: jacyl4）
  │
  ├─ 连接 serve 进程（gRPC 127.0.0.1:9800）
  │   ├─ 成功 → 委托 serve 完成操作 → 返回结果
  │   └─ 失败 → 报错退出（不降级、不自行加载密钥）
  │
  └─ CLI 全程无密钥，无特殊权限
```

- `paraspeech serve` 必须运行（systemd 保证常驻）
- CLI 进程以普通用户运行，没有 setgid 等特殊位
- OpenClaw 无法通过 `cat`、`env`、`/proc` 等任何方式获取密钥
- 更换密钥只需 root 编辑 `secrets.env` + `systemctl reload paraspeech`（SIGHUP 热重载，零停机）

---

## 14. 配置体系

### 14.1 配置文件格式（TOML）

```toml
# /etc/paraspeech/paraspeech.toml

[server]
grpc_addr    = "127.0.0.1:9800"
metrics_addr = "127.0.0.1:9801"    # 单独端口，安全隔离
shutdown_timeout = "10s"

[vault]
secrets_file = "/etc/paraspeech/secrets.env"
enforce_isolation = true

[log]
level  = "info"       # debug, info, warn, error
format = "json"       # json, text
output = "stderr"     # stderr, file
file   = ""           # 仅 output=file 时生效

[stt]
enabled         = true
max_bytes       = 26214400          # 25MB
timeout         = "90s"
default_model   = "gpt-4o-mini-transcribe"
max_concurrent  = 10

[stt.vad]
mode            = "on"              # off, on, debug
hop_size        = 256               # TEN VAD 帧长（样本数），256=16ms / 160=10ms @ 16kHz
threshold       = 0.5               # TEN VAD 检测阈值 [0.0, 1.0]
min_speech_ms   = 200
min_silence_ms  = 300
pad_ms          = 150
max_gap_ms      = 500
max_audio_sec   = 45
min_trim_ratio  = 0.3

[stt.stream]
enabled         = true              # 启用流式转写（Telegram 语音即时处理）
chunk_sec       = 8.0
overlap_sec     = 1.0
max_chunks      = 12

[stt.upstream]
provider        = "openai"
endpoint        = "https://api.openai.com/v1/audio/transcriptions"
connect_timeout = "5s"
read_timeout    = "90s"
max_connections = 20
max_keepalive   = 10
prewarm_on_start = true            # 进程启动时预热上游 HTTP/2 连接
prewarm_on_task  = true            # 每个流式任务开始前确保连接已建立
prewarm_fail_open = true           # 预热失败不阻塞业务，请求阶段兜底建连
prewarm_timeout   = "800ms"        # 预热等待上限，超时立即降级

[tts]
enabled         = true
max_body        = 524288            # 512KB
timeout         = "45s"
default_model   = "gpt-4o-mini-tts"
default_voice   = "nova"
default_speed   = 1.22
default_emotion = "neutral"         # VoiceProfile 默认情感（neutral, cheerful, serious, empathetic）
default_style   = "conversational"  # VoiceProfile 默认风格（conversational, narration, newscast）
default_format  = "opus"
max_sec         = 25.0
max_concurrent  = 10
out_dir         = ""                # 空则不落盘

[tts.upstream]
provider        = "openai"
endpoint        = "https://api.openai.com/v1/audio/speech"
connect_timeout = "5s"
read_timeout    = "45s"
max_connections = 20
max_keepalive   = 10
prewarm_on_start = true
prewarm_on_task  = true
prewarm_fail_open = true
prewarm_timeout   = "800ms"

[queue]
stt_burst       = 20
stt_rate        = 10.0              # req/s
tts_burst       = 20
tts_rate        = 10.0
```

### 14.2 环境变量覆盖

**仅非敏感配置项**支持环境变量覆盖，命名规则 `PARASPEECH_{SECTION}_{KEY}`：

```bash
PARASPEECH_STT_VAD_MODE=debug
PARASPEECH_TTS_DEFAULT_VOICE=alloy
PARASPEECH_SERVER_GRPC_ADDR=127.0.0.1:9800
```

**安全限制**：以下字段**不支持**环境变量覆盖（防止密钥出现在 `/proc/PID/environ`）：
- `vault.secrets_file`（密钥文件路径只从 TOML 或 `--config` 读取）
- 任何含 `key`、`secret`、`token` 的字段

### 14.3 密钥文件格式

```env
# /etc/paraspeech/secrets.env
# 权限：0640 root:paraspeech（OpenClaw 用户不可读）
PARASPEECH_STT_KEY=sk-xxx-stt-dedicated
PARASPEECH_TTS_KEY=sk-xxx-tts-dedicated
```

- 启动时读取到内存，立即关闭 fd，不持续 watch 文件
- **更换密钥**：编辑此文件后 `sudo systemctl reload paraspeech`（SIGHUP 热重载，零停机）
- 热重载失败（权限/格式/隔离检查不过）时保留旧密钥继续服务
- 详细安全设计与轮换流程见 [第 9 节 密钥金库](#9-密钥金库)

---

## 15. 部署方案

### 15.1 systemd unit

```ini
# /etc/systemd/system/paraspeech.service
[Unit]
Description=ParaSpeech unified speech middleware
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=paraspeech
Group=paraspeech
ExecStart=/usr/local/bin/paraspeech serve --config /etc/paraspeech/paraspeech.toml
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=2
StartLimitIntervalSec=300
StartLimitBurst=20
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=false
ReadWritePaths=/var/log/paraspeech /home/jacyl4/.openclaw/media/outbound/paraspeech
LimitCORE=0
Environment=GOTRACEBACK=single

[Install]
WantedBy=multi-user.target
```

### 15.2 安装步骤

```bash
# 1. 编译
make build    # → bin/paraspeech

# 2. 创建专用用户和组
sudo useradd -r -s /usr/sbin/nologin paraspeech

# 3. 安装二进制（普通权限，CLI 无需特殊权限）
sudo install -m 755 bin/paraspeech /usr/local/bin/paraspeech

# 4. 配置（不含密钥，所有用户可读）
sudo mkdir -p /etc/paraspeech
sudo cp configs/paraspeech.toml /etc/paraspeech/
sudo chmod 644 /etc/paraspeech/paraspeech.toml
sudo cp configs/paraspeech.service /etc/systemd/system/

# 5. 密钥文件（仅 root 和 paraspeech 组可读）
sudo touch /etc/paraspeech/secrets.env
sudo chown root:paraspeech /etc/paraspeech/secrets.env
sudo chmod 640 /etc/paraspeech/secrets.env
# sudo vim /etc/paraspeech/secrets.env  ← 填入密钥

# 6. 验证权限（关键！确保 OpenClaw 用户无法直接读取密钥）
sudo -u jacyl4 cat /etc/paraspeech/secrets.env    # 应返回 Permission denied
sudo -u paraspeech cat /etc/paraspeech/secrets.env # 应成功

# 7. 启动
sudo systemctl daemon-reload
sudo systemctl enable --now paraspeech

# 8. 验证
paraspeech health
# 可选：如果启用了 serve（gRPC）再验证端口
grpcurl -plaintext 127.0.0.1:9800 grpc.health.v1.Health/Check
```

### 15.3 CLI wrapper 兼容脚本

替代旧的 `stt-transcribe`：

```bash
#!/usr/bin/env bash
# /usr/local/bin/paraspeech-transcribe
exec paraspeech transcribe "$@"
```

替代旧的 `tts_proxy_synth.py`：

```bash
#!/usr/bin/env bash
# /usr/local/bin/paraspeech-synthesize
exec paraspeech synthesize "$@"
```

OpenClaw 工具配置迁移示例（prototext 格式，与 CLI 输出统一）：

```prototext
tools {
  media {
    audio {
      transcribe {
        command: "/usr/local/bin/paraspeech-transcribe"
        args: "{{MediaPath}}"
      }
      synthesize {
        command: "/usr/local/bin/paraspeech-synthesize"
        args: "--text {{Text}} --voice nova --emotion cheerful"
      }
    }
  }
}
```

---

## 16. 关键实现片段

本节给出核心模块的骨架代码，作为构建指南参考。

### 16.1 入口：依赖组装（cmd/paraspeech/main.go）

```go
func main() {
    cfg := config.Load() // TOML + env overlay

    // Vault: 加载密钥 + SIGHUP 热重载
    v, err := vault.New(cfg.Vault)
    if err != nil {
        log.Fatalf("vault init failed: %v", err)
    }
    go v.WatchReload(syscall.SIGHUP) // 监听 SIGHUP 信号

    // VAD
    var detector vad.Detector
    if cfg.STT.VAD.Mode != "off" {
        detector, err = vad.NewTenVad(cfg.STT.VAD.HopSize, cfg.STT.VAD.Threshold)
        if err != nil {
            log.Printf("warn: TEN VAD init failed, fallback to vad=off: %v", err)
        }
    }
    merger := vad.NewSegmentMerger(cfg.STT.VAD)

    // Providers
    sttProvider := openai.NewSTT(v, cfg.STT.Upstream)
    ttsProvider := openai.NewTTS(v, cfg.TTS.Upstream)

    // Services
    sttSvc := stt.NewService(detector, merger, sttProvider, cfg.STT)
    ttsSvc := tts.NewService(ttsProvider, cfg.TTS)

    // gRPC server
    grpcServer := transport.NewGRPCServer(cfg.Server, sttSvc, ttsSvc)

    // Graceful shutdown
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    go grpcServer.Serve()
    <-ctx.Done()
    grpcServer.GracefulStop(cfg.Server.ShutdownTimeout)
    if detector != nil {
        detector.Close()
    }
}
```

### 16.2 ffmpeg 管道封装（internal/codec/ffmpeg.go）

复用现有 Python stt-proxy 已验证的 ffmpeg 命令行，零磁盘 IO：

```go
// Decode 将任意音频格式解码为 16kHz mono PCM int16
// 输入通过 io.Reader 流式写入 ffmpeg stdin，输出从 stdout 流式读取
func Decode(ctx context.Context, input io.Reader) (io.ReadCloser, error) {
    cmd := exec.CommandContext(ctx,
        "ffmpeg", "-hide_banner", "-loglevel", "error",
        "-i", "pipe:0",        // stdin
        "-ac", "1",            // mono
        "-ar", "16000",        // 16kHz
        "-f", "s16le",         // raw PCM int16 little-endian（省去 WAV 头解析）
        "pipe:1",              // stdout
    )
    cmd.Stdin = input
    stdout, err := cmd.StdoutPipe()
    if err != nil {
        return nil, fmt.Errorf("ffmpeg stdout pipe: %w", err)
    }
    if err := cmd.Start(); err != nil {
        return nil, fmt.Errorf("ffmpeg start: %w", err)
    }
    // 返回 ReadCloser，调用方读完后 Close() 会 wait ffmpeg 退出
    return &ffmpegReader{cmd: cmd, Reader: stdout}, nil
}

// ReadFrames 从 PCM 流中持续读出固定大小帧
func ReadFrames(pcm io.Reader, hopSize int) <-chan []int16 {
    ch := make(chan []int16, 64) // 缓冲 64 帧 ≈ 1s @ 16ms/帧
    go func() {
        defer close(ch)
        buf := make([]byte, hopSize*2) // int16 = 2 bytes
        for {
            _, err := io.ReadFull(pcm, buf)
            if err != nil {
                return
            }
            frame := make([]int16, hopSize)
            for i := range frame {
                frame[i] = int16(binary.LittleEndian.Uint16(buf[i*2:]))
            }
            ch <- frame
        }
    }()
    return ch
}
```

### 16.3 STT 业务编排（internal/stt/service.go）

```go
type Service struct {
    detector vad.Detector
    merger   vad.SegmentMerger
    provider Transcriber
    cfg      config.STT
}

// Transcribe 处理完整音频：解码 → VAD → 裁剪 → 上游转写
func (s *Service) Transcribe(ctx context.Context, audio io.Reader, filename string) (*TranscribeResult, error) {
    // 1. ffmpeg 解码
    pcm, err := codec.Decode(ctx, audio)
    if err != nil {
        return nil, errs.Wrap(errs.ErrSTTDecodeFailed, err)
    }
    defer pcm.Close()

    // 2. VAD（可选）
    var trimmedAudio io.Reader
    var vadMeta *VadMeta
    if s.detector != nil && s.cfg.VAD.Mode != "off" {
        trimmedAudio, vadMeta, err = s.vadProcess(pcm)
        if err != nil {
            // VAD 失败 → 回退原音频
            vadMeta = &VadMeta{Enabled: true, Fallback: true, Reason: fmt.Sprintf("vad_error:%T", err)}
            trimmedAudio = pcm // fallback: 重新解码或用已缓存的数据
        }
    } else {
        trimmedAudio = pcm
        vadMeta = &VadMeta{Enabled: false, Reason: "vad_off"}
    }

    // 3. 上游转写（当前 OpenAI: 批量提交）
    result, err := s.provider.Transcribe(ctx, &TranscribeRequest{
        Audio:    trimmedAudio,
        Filename: filename,
        Model:    s.cfg.DefaultModel,
    })
    if err != nil {
        return nil, errs.Wrap(errs.ErrSTTUpstream, err)
    }
    result.VadMeta = vadMeta
    return result, nil
}

// vadProcess 逐帧 TEN VAD + 段合并 + 裁剪
func (s *Service) vadProcess(pcm io.ReadCloser) (io.Reader, *VadMeta, error) {
    frames := codec.ReadFrames(pcm, s.detector.HopSize())
    var results []vad.FrameResult
    var allSamples []int16

    for frame := range frames {
        allSamples = append(allSamples, frame...)
        fr, err := s.detector.Process(frame)
        if err != nil {
            return nil, nil, err
        }
        results = append(results, *fr)
    }

    segments := s.merger.Merge(results, s.detector.HopSize(), 16000)
    trimmed := extractSegments(allSamples, segments)

    audioMsBefore := int64(len(allSamples)) * 1000 / 16000
    audioMsAfter := int64(len(trimmed)) * 1000 / 16000
    trimRatio := float64(audioMsAfter) / float64(audioMsBefore)

    // 安全检查：裁剪过度 → 回退
    if trimRatio < s.cfg.VAD.MinTrimRatio {
        return samplesToReader(allSamples), &VadMeta{
            Enabled: true, Fallback: true, Reason: "trim_ratio_too_small_fallback",
            AudioMsBefore: audioMsBefore, AudioMsAfter: audioMsBefore,
        }, nil
    }

    return samplesToReader(trimmed), &VadMeta{
        Enabled: true, Reason: "ok",
        AudioMsBefore: audioMsBefore, AudioMsAfter: audioMsAfter,
        TrimRatio: trimRatio, SegmentsCount: int32(len(segments)),
    }, nil
}
```

### 16.4 TTS 文本分段（internal/tts/splitter.go）

保留标点不切，仅在段落/换行边界分段：

```go
// Split 将文本按段落边界切分，保留标点完整性
func Split(text string, maxSec float64) []TextSegment {
    safeSec := maxSec * 0.88 // 安全系数

    // 1. 按段落边界切分（\n\n）
    paragraphs := strings.Split(text, "\n\n")
    var segments []TextSegment

    for _, para := range paragraphs {
        para = strings.TrimSpace(para)
        if para == "" {
            continue
        }
        est := estimateDuration(para)
        if est <= safeSec {
            segments = append(segments, TextSegment{Text: para, EstimatedSec: est})
            continue
        }

        // 2. 段落过长 → 按换行切
        lines := strings.Split(para, "\n")
        for _, line := range lines {
            line = strings.TrimSpace(line)
            if line == "" {
                continue
            }
            est = estimateDuration(line)
            if est <= safeSec {
                segments = append(segments, TextSegment{Text: line, EstimatedSec: est})
                continue
            }

            // 3. 单行仍过长 → 仅在句末标点后切（。！？.!?）
            segments = append(segments, splitAtSentenceEnd(line, safeSec)...)
        }
    }
    return segments
}

// estimateDuration 移植自 Python _estimate_sec()
func estimateDuration(text string) float64 {
    var sec float64
    var cjk, latin, digits, pauses int
    // ... 统计各类字符数 ...
    sec += float64(cjk) / 4.2
    sec += float64(latin) / 2.6
    sec += float64(digits) / 5.0
    sec += float64(pauses) * 0.18
    if sec < 0.4 {
        sec = 0.4
    }
    return sec
}
```

### 16.5 Vault SIGHUP 热重载（internal/vault/vault.go）

```go
type fileVault struct {
    mu       sync.RWMutex
    keys     map[KeyPurpose][]byte // []byte 而非 string，方便 memzero
    cfg      config.Vault
    loaded   bool
}

func (v *fileVault) GetKey(purpose KeyPurpose) (string, error) {
    v.mu.RLock()
    defer v.mu.RUnlock()
    k, ok := v.keys[purpose]
    if !ok {
        return "", fmt.Errorf("key not found for purpose %d", purpose)
    }
    return string(k), nil
}

// WatchReload 监听 SIGHUP 信号，触发密钥热重载
func (v *fileVault) WatchReload(sig os.Signal) {
    ch := make(chan os.Signal, 1)
    signal.Notify(ch, sig)
    for range ch {
        if err := v.reload(); err != nil {
            slog.Error("vault reload failed, keeping old keys", "error", err)
        } else {
            slog.Info("vault reloaded successfully")
        }
    }
}

func (v *fileVault) reload() error {
    newKeys, err := loadAndValidate(v.cfg)
    if err != nil {
        return err // 保留旧密钥
    }
    v.mu.Lock()
    oldKeys := v.keys
    v.keys = newKeys
    v.mu.Unlock()
    memzero(oldKeys) // 覆写旧密钥
    return nil
}

func loadAndValidate(cfg config.Vault) (map[KeyPurpose][]byte, error) {
    // 1. 权限预检
    info, err := os.Stat(cfg.SecretsFile)
    if err != nil {
        return nil, fmt.Errorf("secrets file: %w", err)
    }
    if info.Mode().Perm()&0o004 != 0 { // world-readable
        return nil, fmt.Errorf("secrets file %s is world-readable (mode %o), refusing", cfg.SecretsFile, info.Mode().Perm())
    }

    // 2. 读取 + 解析
    data, err := os.ReadFile(cfg.SecretsFile)
    if err != nil {
        return nil, fmt.Errorf("read secrets: %w", err)
    }
    keys := parseEnvFile(data)

    // 3. 隔离检查
    if cfg.EnforceIsolation {
        if bytes.Equal(keys[KeySTT], keys[KeyTTS]) {
            memzero(keys)
            return nil, fmt.Errorf("STT and TTS keys must be different (isolation enforced)")
        }
    }

    // 4. mlock
    for _, k := range keys {
        unix.Mlock(k)
    }
    return keys, nil
}

// memzero 覆写密钥字节，防止残留在内存中
func memzero(keys map[KeyPurpose][]byte) {
    for _, k := range keys {
        for i := range k {
            k[i] = 0
        }
    }
}

// String 实现 fmt.Stringer，防止密钥在 panic/日志中泄漏
func (v *fileVault) String() string {
    return "[vault:REDACTED]"
}
```

### 16.6 Provider HTTP 客户端（internal/provider/openai/client.go）

```go
// SharedClient 为 STT/TTS provider 提供共享的 HTTP/2 连接池
type SharedClient struct {
    client *http.Client
    vault  vault.Vault
}

func NewSharedClient(v vault.Vault, cfg config.Upstream) *SharedClient {
    transport := &http.Transport{
        MaxIdleConns:        cfg.MaxConnections,
        MaxIdleConnsPerHost: cfg.MaxKeepalive,
        IdleConnTimeout:     90 * time.Second,
        ForceAttemptHTTP2:   true,
    }
    return &SharedClient{
        client: &http.Client{
            Transport: transport,
            Timeout:   cfg.ReadTimeout,
        },
        vault: v,
    }
}

// Prewarm 预热连接：完成 DNS + TCP + TLS + HTTP/2 握手
func (c *SharedClient) Prewarm(ctx context.Context, endpoint string, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    req, _ := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
    resp, err := c.client.Do(req)
    if resp != nil {
        resp.Body.Close()
    }
    return err
}
```

### 16.7 优雅停机（cmd/paraspeech/main.go 片段）

```go
// GracefulStop 先停接新请求，等在途请求完成，超时强杀
func (s *GRPCServer) GracefulStop(timeout time.Duration) {
    slog.Info("shutting down, draining requests...", "timeout", timeout)

    // 1. 停止接受新连接
    s.grpc.GracefulStop()

    // 2. 等待在途请求或超时
    done := make(chan struct{})
    go func() {
        s.wg.Wait() // WaitGroup 跟踪在途请求
        close(done)
    }()
    select {
    case <-done:
        slog.Info("all requests drained")
    case <-time.After(timeout):
        slog.Warn("shutdown timeout, forcing stop")
        s.grpc.Stop()
    }
}
```

---

## 17. 构建与测试

### 17.1 Makefile 目标

```makefile
.PHONY: build test lint proto clean

build:
	go build -o bin/paraspeech ./cmd/paraspeech

test:
	go test ./internal/... -race -cover -count=1

lint:
	golangci-lint run ./...

proto:
	buf generate api/proto

clean:
	rm -rf bin/
```

### 17.2 测试策略

| 层级 | 覆盖范围 | 工具 |
|------|---------|------|
| 单元测试 | vad/segment、tts/splitter、tts/sanitizer、errs、vault | `go test` |
| 集成测试 | service + mock provider | `go test` + testify mock |
| 端到端 | CLI → gRPC → serve 全链路 | `go test` + testcontainers 或手动 |
| 基准测试 | VAD 吞吐、分段性能 | `go test -bench` |

### 17.3 关键单元测试清单

- `vad/segment_test.go`：各种 voice flag 模式的合并结果
- `tts/splitter_test.go`：中英混合文本分段、时长估算精度
- `tts/sanitizer_test.go`：markdown 各种格式清洗
- `vault/vault_test.go`：隔离检查、缺失密钥、正常加载、权限校验（拒绝 world-readable 文件）、Stringer redact 验证
- `errs/codes_test.go`：错误码到 gRPC status 映射

---

## 18. 迁移路线

分三阶段，每阶段可独立回退。

### 阶段 1：并行运行（1-2 周）

```
目标：ParaSpeech 启动，与旧服务并行，验证功能等价

步骤：
1. 编译部署 paraspeech，监听新端口（gRPC 9800）
2. 旧服务（stt-proxy:8765, tts-proxy:8766）继续运行
3. 手动对比测试：同一音频分别发到新旧服务，比较结果
4. 验收：>=50 条样本，转写结果一致率 >99%
```

### 阶段 2：切换流量（3-5 天）

```
目标：OpenClaw 指向 ParaSpeech

步骤：
1. 修改 OpenClaw 工具配置，切换到 `paraspeech-transcribe` / `paraspeech-synthesize`
   CLI 通过 gRPC 委托 serve 进程
2. 旧服务降为 standby（不接流量但保持运行）
3. 监控 paraspeech 指标 48 小时
4. 异常则一键切回旧端口
```

### 阶段 3：清理旧服务（1-2 天）

```
目标：移除旧 Python 服务

步骤：
1. sudo systemctl disable --now stt-proxy tts-proxy
2. 确认 paraspeech 持续稳定 72 小时
3. 归档旧代码到 /opt/stt-proxy.archive、/opt/tts-proxy.archive
4. 清理旧 systemd unit
5. 更新 OpenClaw workspace 文档指向 paraspeech
```

---

## 19. 风险与缓解

| 风险 | 严重度 | 缓解 |
|------|--------|------|
| **OpenClaw prompt injection 窃取密钥** | **高** | 密钥文件 `0640 root:paraspeech`，OpenClaw 用户无法 `cat`；密钥不通过环境变量传递（`/proc/PID/environ` 无密钥）；CLI 纯委托模式，进程内完全无密钥；vault 结构体 `fmt.Stringer` 返回 `[REDACTED]`；core dump 禁用 |
| serve 进程不可达导致 CLI 不可用 | 中 | systemd `Restart=always` 保证 serve 常驻；CLI 报错信息明确指引用户启动服务；健康检查可监控 |
| SIGHUP 热重载期间请求处理 | 低 | 原子替换密钥 map（`mu.Lock`），在途请求不受影响；校验失败时保留旧密钥 |
| TEN VAD CGo 绑定稳定性 | 中 | TEN VAD 官方提供 Go 示例（examples/go-tenvad）；阶段 1 逐样本对比 Go CGo 与 Python 结果一致性；libten_vad 为预编译库，无额外编译依赖 |
| 单进程故障影响 STT+TTS 双通道 | 中 | Restart=always + 健康检查 + 独立 goroutine 隔离 |
| gRPC 生态与 OpenClaw Node.js 对接成本 | 低 | 优先通过 CLI wrapper 接入，避免业务侧直接对接 gRPC |
| ffmpeg 管道在高并发下的 fd 泄漏 | 中 | 封装 codec 层确保 defer close；单元测试覆盖 |
| TOML 配置迁移遗漏 | 低 | 环境变量覆盖兜底（仅非敏感项）；启动时打印生效配置摘要 |
| 旧 CLI wrapper 调用方未更新 | 低 | 保留 `paraspeech-transcribe` 兼容脚本 |

---

## 20. 附录：协议定义草案

所有 CLI 输出均为 **prototext** 格式（可选 `--format json` 切换）。

### 20.1 统一健康检查响应（`paraspeech health`）

```prototext
ok: true
service: "paraspeech"
version: "0.1.0"
channels {
  stt {
    enabled: true
    model: "gpt-4o-mini-transcribe"
    vad_mode: "on"
    vault_ready: true
  }
  tts {
    enabled: true
    model: "gpt-4o-mini-tts"
    voice_profile {
      voice: "nova"
      speed: 1.22
      emotion: "neutral"
    }
    format: "opus"
    vault_ready: true
  }
}
stream {
  chunk_sec: 8.0
  overlap_sec: 1.0
  max_chunks: 12
}
```

### 20.2 `TranscribeResponse`（CLI 输出示例）

正常（极简，仅一行，最省 token）：
```prototext
text: "你好"
```

debug 模式（`--vad-debug`）：
```prototext
text: "你好"
meta {
  trace_id: "abc123"
  process_ms: 1180
  model: "gpt-4o-mini-transcribe"
  vad {
    enabled: true
    reason: "ok"
    fallback: false
    audio_ms_before: 5200
    audio_ms_after: 3100
    trim_ratio: 0.5962
    segments_count: 2
    elapsed_ms: 45
  }
}
```

### 20.3 `SynthesizeResponse`（CLI 输出示例）

正常：
```prototext
count: 1
segments {
  index: 1
  text: "你好"
  estimated_sec: 0.48
  path: "/home/jacyl4/.openclaw/media/outbound/paraspeech/telegram-main/20260215-100000/seg-01.opus"
  size_bytes: 4096
  content_type: "audio/opus"
}
meta {
  trace_id: "abc123"
  process_ms: 890
  voice_profile {
    voice: "nova"
    speed: 1.22
    emotion: "neutral"
  }
}
```

失败（stderr，保留 `fallback_marker` 兼容）：
```prototext
error {
  code: 310
  name: "TTS_UPSTREAM"
  message: "upstream returned HTTP 429"
  trace_id: "abc123"
}
fallback: "text_only"
fallback_marker: "（语音生成失败，已切回文本模式演示）"
```

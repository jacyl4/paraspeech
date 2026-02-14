package grpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	pb "paraspeech/api/proto/paraspeech/v1"
	"paraspeech/internal/observe"
	"paraspeech/internal/stt"

	gogrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type sttHandler struct {
	pb.UnimplementedSTTServiceServer
	svc *stt.Service
	wg  *sync.WaitGroup
}

func registerSTTHandler(s *gogrpc.Server, svc *stt.Service, wg *sync.WaitGroup) {
	pb.RegisterSTTServiceServer(s, &sttHandler{svc: svc, wg: wg})
}

func (h *sttHandler) Transcribe(ctx context.Context, req *pb.TranscribeRequest) (*pb.TranscribeResponse, error) {
	h.wg.Add(1)
	defer h.wg.Done()

	traceID := observe.NewTraceID()
	start := time.Now()

	if len(req.GetAudio()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty audio")
	}

	result, err := h.svc.Transcribe(
		ctx,
		bytes.NewReader(req.GetAudio()),
		req.GetFilename(),
		req.GetDurationHint(),
		int64(len(req.GetAudio())),
		req.GetLanguage(),
		req.GetModel(),
		"",
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "transcribe: %v", err)
	}
	result.DurationMS = time.Since(start).Milliseconds()

	resp := &pb.TranscribeResponse{
		Text: result.Text,
		Meta: &pb.TranscribeMeta{
			TraceId:   traceID,
			ProcessMs: result.DurationMS,
		},
	}
	if result.VadMeta != nil {
		resp.Meta.Vad = toPBVadMeta(result.VadMeta)
	}
	return resp, nil
}

func (h *sttHandler) TranscribeStream(stream gogrpc.BidiStreamingServer[pb.AudioFrame, pb.TranscribeEvent]) error {
	h.wg.Add(1)
	defer h.wg.Done()

	traceID := observe.NewTraceID()
	start := time.Now()

	firstFrame, err := stream.Recv()
	if err == io.EOF {
		return status.Error(codes.InvalidArgument, "empty audio stream")
	}
	if err != nil {
		return status.Errorf(codes.Internal, "recv first frame: %v", err)
	}

	language := strings.TrimSpace(firstFrame.GetLanguage())
	model := strings.TrimSpace(firstFrame.GetModel())
	vadDebug := firstFrame.GetVadDebug()

	// Backward compatibility: old clients may still pass metadata.
	mdLanguage, mdModel, mdVadDebug := streamMetadata(stream.Context())
	if language == "" {
		language = mdLanguage
	}
	if model == "" {
		model = mdModel
	}
	if !vadDebug {
		vadDebug = mdVadDebug
	}

	durationHint := firstFrame.GetDurationHint()

	var audioReader io.Reader
	var audioSize int64
	if durationHint <= 0 {
		buffer := bytes.NewBuffer(nil)
		if len(firstFrame.GetData()) > 0 {
			if _, err := buffer.Write(firstFrame.GetData()); err != nil {
				return status.Errorf(codes.Internal, "buffer first frame: %v", err)
			}
		}
		if !firstFrame.GetEndOfAudio() {
			for {
				frame, recvErr := stream.Recv()
				if recvErr == io.EOF {
					break
				}
				if recvErr != nil {
					return status.Errorf(codes.Internal, "recv frame: %v", recvErr)
				}
				if len(frame.GetData()) > 0 {
					if _, err := buffer.Write(frame.GetData()); err != nil {
						return status.Errorf(codes.Internal, "buffer frame: %v", err)
					}
				}
				if frame.GetEndOfAudio() {
					break
				}
			}
		}
		audioSize = int64(buffer.Len())
		audioReader = bytes.NewReader(buffer.Bytes())
	} else {
		pr, pw := io.Pipe()
		writeFrame := func(frame *pb.AudioFrame) error {
			if len(frame.GetData()) > 0 {
				n, err := pw.Write(frame.GetData())
				audioSize += int64(n)
				if err != nil {
					return err
				}
			}
			if frame.GetEndOfAudio() {
				return io.EOF
			}
			return nil
		}

		if err := writeFrame(firstFrame); err != nil {
			if err == io.EOF {
				_ = pw.Close()
			} else {
				_ = pw.CloseWithError(err)
				return status.Errorf(codes.Internal, "write first frame: %v", err)
			}
		}

		if !firstFrame.GetEndOfAudio() {
			go func() {
				defer pw.Close()
				for {
					frame, recvErr := stream.Recv()
					if recvErr == io.EOF {
						return
					}
					if recvErr != nil {
						_ = pw.CloseWithError(fmt.Errorf("recv frame: %w", recvErr))
						return
					}
					if writeErr := writeFrame(frame); writeErr != nil {
						if writeErr == io.EOF {
							return
						}
						_ = pw.CloseWithError(fmt.Errorf("write frame: %w", writeErr))
						return
					}
				}
			}()
		}
		audioReader = pr
	}

	eventCh := make(chan *stt.TranscribeEvent, 64)
	errCh := make(chan error, 1)
	go func() {
		defer close(eventCh)
		errCh <- h.svc.TranscribeStream(stream.Context(), audioReader, "audio.bin", durationHint, audioSize, language, model, "", eventCh)
	}()

	for event := range eventCh {
		var pbEvent pb.TranscribeEvent
		switch event.Type {
		case stt.EventPartial:
			pbEvent.Event = &pb.TranscribeEvent_Partial{
				Partial: &pb.PartialResult{
					Text:            event.Text,
					AccumulatedText: event.AccumulatedText,
				},
			}
		case stt.EventFinal:
			final := &pb.FinalResult{Text: event.Text}
			if vadDebug {
				final.Meta = &pb.TranscribeMeta{
					TraceId:   traceID,
					ProcessMs: time.Since(start).Milliseconds(),
				}
				if event.VadMeta != nil {
					final.Meta.Vad = toPBVadMeta(event.VadMeta)
				}
			}
			pbEvent.Event = &pb.TranscribeEvent_Final{Final: final}
		default:
			continue
		}
		if err := stream.Send(&pbEvent); err != nil {
			return status.Errorf(codes.Internal, "send event: %v", err)
		}
	}

	if err := <-errCh; err != nil {
		return status.Errorf(codes.Internal, "transcribe stream: %v", err)
	}
	return nil
}

func toPBVadMeta(meta *stt.VadMeta) *pb.VadMeta {
	if meta == nil {
		return nil
	}
	return &pb.VadMeta{
		Enabled:       meta.Enabled,
		Reason:        meta.Reason,
		Fallback:      meta.Fallback,
		AudioMsBefore: meta.AudioMsBefore,
		AudioMsAfter:  meta.AudioMsAfter,
		TrimRatio:     meta.TrimRatio,
		SegmentsCount: meta.SegmentsCount,
		ElapsedMs:     meta.ElapsedMS,
	}
}

func streamMetadata(ctx context.Context) (language, model string, vadDebug bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", "", false
	}
	language = firstMDValue(md, "x-stt-language")
	model = firstMDValue(md, "x-stt-model")
	if strings.EqualFold(firstMDValue(md, "x-stt-vad-debug"), "true") {
		vadDebug = true
	}
	return language, model, vadDebug
}

func firstMDValue(md metadata.MD, key string) string {
	vals := md.Get(strings.ToLower(key))
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

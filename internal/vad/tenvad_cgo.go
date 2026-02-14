//go:build cgo

package vad

/*
#cgo linux LDFLAGS: -ldl
#include <dlfcn.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef void* ten_vad_handle_t;
typedef int (*ten_vad_create_fn)(ten_vad_handle_t*, size_t, float);
typedef int (*ten_vad_process_fn)(ten_vad_handle_t, int16_t*, size_t, float*, int*);
typedef void (*ten_vad_destroy_fn)(ten_vad_handle_t*);

static void* ten_lib = NULL;
static ten_vad_create_fn p_create = NULL;
static ten_vad_process_fn p_process = NULL;
static ten_vad_destroy_fn p_destroy = NULL;
static char ten_last_err[256] = {0};

static void ten_set_err(const char* msg) {
	if (msg == NULL) {
		ten_last_err[0] = '\0';
		return;
	}
	snprintf(ten_last_err, sizeof(ten_last_err), "%s", msg);
}

static const char* tenvad_last_error() {
	if (ten_last_err[0] == '\0') {
		return "unknown error";
	}
	return ten_last_err;
}

static int tenvad_load(const char* custom_path) {
	if (p_create != NULL && p_process != NULL && p_destroy != NULL) {
		return 0;
	}

	const char* candidates[8] = {0};
	int n = 0;
	if (custom_path != NULL && custom_path[0] != '\0') {
		candidates[n++] = custom_path;
	}
	candidates[n++] = "libten_vad.so";
	candidates[n++] = "/usr/local/lib/libten_vad.so";
	candidates[n++] = "/usr/lib/libten_vad.so";
	candidates[n++] = "/usr/lib64/libten_vad.so";

	for (int i = 0; i < n; i++) {
		ten_lib = dlopen(candidates[i], RTLD_LAZY | RTLD_LOCAL);
		if (ten_lib != NULL) {
			break;
		}
	}
	if (ten_lib == NULL) {
		const char* e = dlerror();
		ten_set_err(e != NULL ? e : "dlopen libten_vad.so failed");
		return -1;
	}

	p_create = (ten_vad_create_fn)dlsym(ten_lib, "ten_vad_create");
	p_process = (ten_vad_process_fn)dlsym(ten_lib, "ten_vad_process");
	p_destroy = (ten_vad_destroy_fn)dlsym(ten_lib, "ten_vad_destroy");
	if (p_create == NULL || p_process == NULL || p_destroy == NULL) {
		ten_set_err("dlsym ten_vad_* symbols failed");
		dlclose(ten_lib);
		ten_lib = NULL;
		p_create = NULL;
		p_process = NULL;
		p_destroy = NULL;
		return -2;
	}
	return 0;
}

static int tenvad_create(ten_vad_handle_t* h, size_t hop_size, float threshold) {
	if (p_create == NULL) {
		return -999;
	}
	return p_create(h, hop_size, threshold);
}

static int tenvad_process(ten_vad_handle_t h, int16_t* frame, size_t hop_size, float* prob, int* flag) {
	if (p_process == NULL) {
		return -999;
	}
	return p_process(h, frame, hop_size, prob, flag);
}

static void tenvad_destroy(ten_vad_handle_t* h) {
	if (p_destroy != NULL) {
		p_destroy(h);
	}
}
*/
import "C"

import (
	"fmt"
	"os"
	"sync"
	"unsafe"
)

var (
	tenLoadOnce sync.Once
	tenLoadErr  error
)

type tenVad struct {
	handle  C.ten_vad_handle_t
	hopSize int
}

func ensureTenVadLoaded() error {
	tenLoadOnce.Do(func() {
		custom := os.Getenv("TEN_VAD_LIB")
		cpath := C.CString(custom)
		defer C.free(unsafe.Pointer(cpath))

		if ret := C.tenvad_load(cpath); ret != 0 {
			tenLoadErr = fmt.Errorf("%w: %s", ErrVadNotAvailable, C.GoString(C.tenvad_last_error()))
		}
	})
	return tenLoadErr
}

// NewTenVad creates a TEN VAD detector backed by libten_vad via dlopen+dlsym.
func NewTenVad(hopSize int, threshold float32) (Detector, error) {
	if hopSize != 160 && hopSize != 256 {
		return nil, fmt.Errorf("invalid hop size %d (expected 160 or 256)", hopSize)
	}
	if threshold < 0 || threshold > 1 {
		return nil, fmt.Errorf("invalid threshold %.3f (expected 0.0..1.0)", threshold)
	}
	if err := ensureTenVadLoaded(); err != nil {
		return nil, err
	}

	var h C.ten_vad_handle_t
	ret := C.tenvad_create(&h, C.size_t(hopSize), C.float(threshold))
	if ret != 0 {
		return nil, fmt.Errorf("ten_vad_create failed: %d", int(ret))
	}
	return &tenVad{handle: h, hopSize: hopSize}, nil
}

func (v *tenVad) Process(frame []int16) (*FrameResult, error) {
	if v == nil || v.handle == nil {
		return nil, fmt.Errorf("ten vad handle is nil")
	}
	if len(frame) < v.hopSize {
		return nil, fmt.Errorf("frame too short: got %d want >= %d", len(frame), v.hopSize)
	}
	var prob C.float
	var flag C.int
	ret := C.tenvad_process(
		v.handle,
		(*C.int16_t)(unsafe.Pointer(&frame[0])),
		C.size_t(v.hopSize),
		&prob,
		&flag,
	)
	if ret != 0 {
		return nil, fmt.Errorf("ten_vad_process failed: %d", int(ret))
	}
	return &FrameResult{Probability: float32(prob), IsVoice: int(flag) == 1}, nil
}

func (v *tenVad) HopSize() int {
	if v == nil {
		return 0
	}
	return v.hopSize
}

func (v *tenVad) Close() error {
	if v == nil || v.handle == nil {
		return nil
	}
	C.tenvad_destroy(&v.handle)
	v.handle = nil
	return nil
}

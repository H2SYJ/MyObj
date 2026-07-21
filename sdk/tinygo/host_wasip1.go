//go:build wasip1

package myobjplugin

import (
	"encoding/json"
	"fmt"
	"unsafe"
)

//go:wasmimport myobj http_request
func hostHTTPRequest(requestPtr, requestLen, outputPtr, outputCap uint32) int32

//go:wasmimport myobj file_get
func hostFileGet(requestPtr, requestLen, outputPtr, outputCap uint32) int32

//go:wasmimport myobj files_query
func hostFilesQuery(requestPtr, requestLen, outputPtr, outputCap uint32) int32

func callHTTPRequest(request, response interface{}, capacity int) error {
	requestBytes, output, err := prepareHostCall(request, capacity)
	if err != nil {
		return err
	}
	written := hostHTTPRequest(pointer(requestBytes), uint32(len(requestBytes)), pointer(output), uint32(len(output)))
	return finishHostCall(output, written, response)
}

func callFileGet(request, response interface{}, capacity int) error {
	requestBytes, output, err := prepareHostCall(request, capacity)
	if err != nil {
		return err
	}
	written := hostFileGet(pointer(requestBytes), uint32(len(requestBytes)), pointer(output), uint32(len(output)))
	return finishHostCall(output, written, response)
}

func callFilesQuery(request, response interface{}, capacity int) error {
	requestBytes, output, err := prepareHostCall(request, capacity)
	if err != nil {
		return err
	}
	written := hostFilesQuery(pointer(requestBytes), uint32(len(requestBytes)), pointer(output), uint32(len(output)))
	return finishHostCall(output, written, response)
}

func prepareHostCall(request interface{}, capacity int) ([]byte, []byte, error) {
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, nil, err
	}
	if len(requestBytes) == 0 {
		requestBytes = []byte("{}")
	}
	if capacity <= 0 {
		return nil, nil, fmt.Errorf("host_call_invalid_capacity")
	}
	return requestBytes, make([]byte, capacity), nil
}

func finishHostCall(output []byte, written int32, response interface{}) error {
	if written < 0 || int(written) > len(output) {
		return fmt.Errorf("host_call_failed")
	}
	return json.Unmarshal(output[:written], response)
}

func pointer(value []byte) uint32 {
	return uint32(uintptr(unsafe.Pointer(&value[0])))
}

//go:build !wasip1

package myobjplugin

import "fmt"

func callHTTPRequest(_, _ interface{}, _ int) error { return fmt.Errorf("host_call_requires_wasip1") }
func callFileGet(_, _ interface{}, _ int) error     { return fmt.Errorf("host_call_requires_wasip1") }
func callFilesQuery(_, _ interface{}, _ int) error  { return fmt.Errorf("host_call_requires_wasip1") }

// Tencent is pleased to support the open source community by making bkunifylogbeat 蓝鲸日志采集器 available.
//
// Copyright (C) 2021 THL A29 Limited, a Tencent company.  All rights reserved.
//
// bkunifylogbeat 蓝鲸日志采集器 is licensed under the MIT License.
//
// License for bkunifylogbeat 蓝鲸日志采集器:
// --------------------------------------------------------------------
// Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
// documentation files (the "Software"), to deal in the Software without restriction, including without limitation
// the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software,
// and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all copies or substantial
// portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT
// LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN
// NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
// SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

package wineventlog

import (
	"expvar"
	"fmt"
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/winlogbeat/eventlog"
	"reflect"
	"strconv"
	"strings"
	"syscall"

	"github.com/elastic/beats/winlogbeat/sys"
)

// ToMapStr returns a new MapStr containing the data from this Record.
func ToEvent(e eventlog.Record) beat.Event {
	// Windows Log Specific data
	win := common.MapStr{
		"channel":       e.Channel,
		"event_id":      fmt.Sprint(e.EventIdentifier.ID),
		"provider_name": e.Provider.Name,
		"record_id":     e.RecordID,
		"task":          e.Task,
		"api":           e.API,
	}
	addOptional(win, "computer_name", e.Computer)
	addOptional(win, "kernel_time", e.Execution.KernelTime)
	addOptional(win, "keywords", strings.Join(e.Keywords, ","))
	addOptional(win, "opcode", e.Opcode)
	addOptional(win, "processor_id", e.Execution.ProcessorID)
	addOptional(win, "processor_time", e.Execution.ProcessorTime)
	addOptional(win, "provider_guid", e.Provider.GUID)
	addOptional(win, "session_id", e.Execution.SessionID)
	addOptional(win, "task", e.Task)
	addOptional(win, "user_time", e.Execution.UserTime)
	addOptional(win, "version", e.Version)
	addOptional(win, "event_created", e.TimeCreated.SystemTime)
	// Correlation
	addOptional(win, "activity_id", e.Correlation.ActivityID)
	addOptional(win, "related_activity_id", e.Correlation.RelatedActivityID)
	// Execution
	addOptional(win, "process_pid", e.Execution.ProcessID)
	addOptional(win, "process_thread_id", e.Execution.ThreadID)

	if e.User.Identifier != "" {
		addOptional(win, "user_identifier", e.User.Identifier)
		addOptional(win, "user_name", e.User.Name)
		addOptional(win, "user_domain", e.User.Domain)
		addOptional(win, "user_type", e.User.Type.String())
	}

	addPairs(win, "event_data", e.EventData.Pairs)
	userData := addPairs(win, "user_data", e.UserData.Pairs)
	addOptional(userData, "xml_name", e.UserData.Name.Local)

	// ECS data
	addOptional(win, "event_kind", "event")
	addOptional(win, "event_code", e.EventIdentifier.ID)
	addOptional(win, "event_action", e.Task)

	addOptional(win, "log_level", strings.ToLower(e.Level))
	addOptional(win, "data", sys.RemoveWindowsLineEndings(e.Message))
	// Errors
	addOptional(win, "error_code", e.RenderErrorCode)
	if len(e.RenderErr) == 1 {
		addOptional(win, "error_message", e.RenderErr[0])
	} else {
		addOptional(win, "error_message", e.RenderErr)
	}

	addOptional(win, "event_original", e.XML)

	return beat.Event{
		Timestamp: e.TimeCreated.SystemTime,
		Fields:    win,
		Private:   e.Offset,
	}
}

// addOptional adds a key and value to the given MapStr if the value is not the
// zero value for the type of v. It is safe to call the function with a nil
// MapStr.
func addOptional(m common.MapStr, key string, v interface{}) {
	if m != nil && !isZero(v) {
		m.Put(key, v)
	}
}

// addPairs adds a new dictionary to the given MapStr. The key/value pairs are
// added to the new dictionary. If any keys are duplicates, the first key/value
// pair is added and the remaining duplicates are dropped.
//
// The new dictionary is added to the given MapStr and it is also returned for
// convenience purposes.
func addPairs(m common.MapStr, key string, pairs []sys.KeyValue) common.MapStr {
	if len(pairs) == 0 {
		return nil
	}

	h := make(common.MapStr, len(pairs))
	for i, kv := range pairs {
		// Ignore empty values.
		if kv.Value == "" {
			continue
		}

		// If the key name is empty or if it the default of "Data" then
		// assign a generic name of paramN.
		k := kv.Key
		if k == "" || k == "Data" {
			k = fmt.Sprintf("param%d", i+1)
		}

		// Do not overwrite.
		_, exists := h[k]
		if !exists {
			h[k] = sys.RemoveWindowsLineEndings(kv.Value)
		} else {
			logp.Debug("wineventlog", "Dropping key/value (k=%s, v=%s) pair because key already "+
				"exists. event=%+v", k, kv.Value, m)
		}
	}

	if len(h) == 0 {
		return nil
	}

	m[key] = h
	return h
}

// isZero return true if the given value is the zero value for its type.
func isZero(i interface{}) bool {
	v := reflect.ValueOf(i)
	switch v.Kind() {
	case reflect.Array, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// incrementMetric increments a value in the specified expvar.Map. The key
// should be a windows syscall.Errno or a string. Any other types will be
// reported under the "other" key.
func incrementMetric(v *expvar.Map, key interface{}) {
	switch t := key.(type) {
	default:
		v.Add("other", 1)
	case string:
		v.Add(t, 1)
	case syscall.Errno:
		v.Add(strconv.Itoa(int(t)), 1)
	}
}

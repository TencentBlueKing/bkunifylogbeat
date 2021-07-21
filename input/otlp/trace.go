package otlp

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/TencentBlueKing/collector-go-sdk/v2/bkbeat/beat"
	"github.com/elastic/beats/filebeat/harvester"
	"github.com/elastic/beats/filebeat/util"
	"github.com/elastic/beats/libbeat/common"
	"github.com/gogo/protobuf/jsonpb"
	collectorTrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	v11 "go.opentelemetry.io/proto/otlp/common/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"reflect"
	"time"
)

type TraceService struct {
	forwarder *harvester.Forwarder
	collectorTrace.UnimplementedTraceServiceServer
	marshaler jsonpb.Marshaler
}

func NewTraceService(forwarder *harvester.Forwarder) *TraceService {
	return &TraceService{
		forwarder: forwarder,
		marshaler: jsonpb.Marshaler{},
	}
}

func (t *TraceService) Export(ctx context.Context, req *collectorTrace.ExportTraceServiceRequest) (*collectorTrace.ExportTraceServiceResponse, error) {
	req.GetResourceSpans()
	resourceSpans := req.GetResourceSpans()
	if resourceSpans == nil {
		return &collectorTrace.ExportTraceServiceResponse{}, nil
	}
	for _, resourceSpan := range resourceSpans {
		instrumentationLibrarySpan := resourceSpan.GetInstrumentationLibrarySpans()

		if instrumentationLibrarySpan == nil {
			continue
		}

		for _, instrumentationLibrarySpan := range instrumentationLibrarySpan {
			spans := instrumentationLibrarySpan.GetSpans()
			if spans == nil {
				continue
			}
			for _, span := range spans {
				e := util.NewData()
				buf := bytes.Buffer{}
				err := t.marshaler.Marshal(&buf, span)
				if err != nil {
					continue
				}
				message := common.MapStr{
					"span_name":      span.Name,
					"parent_span_id": formatTraceId(span.ParentSpanId),
					"span_id":        formatSpanId(span.SpanId),
					"trace_id":       formatTraceId(span.TraceId),
					"kind":           span.Kind,
					"start_time":     span.StartTimeUnixNano,
					"end_time":       span.EndTimeUnixNano,
					"attributes":     transformAttributes(span.Attributes),
					"links":          transformLinks(span),
					"events":         transformEvents(span),
					"trace_state":    span.TraceState,
					"status":         span.Status,
				}
				messageContent, err := json.Marshal(&message)
				if err != nil {
					continue
				}
				e.Event = beat.Event{
					Timestamp: time.Now(),
					Fields: common.MapStr{
						"message": string(messageContent),
					},
				}
				t.forwarder.Send(e)
			}
		}
	}
	return &collectorTrace.ExportTraceServiceResponse{}, nil
}

func toValue(valueWrap interface{}) interface{} {
	rType := reflect.TypeOf(valueWrap)
	rVal := reflect.ValueOf(valueWrap)
	if rType.Kind() == reflect.Ptr {
		rType = rType.Elem()
		rVal = rVal.Elem()
	}
	for i := 0; i < rVal.NumField(); i++ {
		return rVal.Field(i).Interface()
	}
	return nil
}

func transformEvents(span *trace.Span) []common.MapStr {
	var result []common.MapStr
	result = make([]common.MapStr, 0)
	for _, event := range span.Events {
		result = append(result, common.MapStr{
			"name":       event.Name,
			"timestamp":  event.TimeUnixNano,
			"attributes": transformAttributes(event.Attributes),
		})
	}
	return result
}

func transformLinks(span *trace.Span) []common.MapStr {
	var result []common.MapStr
	result = make([]common.MapStr, 0)
	for _, link := range span.Links {
		result = append(result, common.MapStr{
			"trace_id":    formatTraceId(link.TraceId),
			"span_id":     formatSpanId(link.SpanId),
			"trace_state": link.TraceState,
			"attributes":  transformAttributes(link.Attributes),
		})
	}
	return result

}

func formatTraceId(traceId []byte) string {
	return hex.EncodeToString(traceId)
}

func formatSpanId(spanId []byte) string {
	return hex.EncodeToString(spanId)
}

func transformAttributes(attributes []*v11.KeyValue) common.MapStr {
	var result = make(common.MapStr)
	for _, attribute := range attributes {
		result[attribute.Key] = toValue(attribute.Value.Value)
	}
	return result
}

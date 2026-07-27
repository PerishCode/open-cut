package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// errorLogBodyBound caps how much of an error-response body one log line may
// carry; error bodies are already client-visible JSON, so logging them adds no
// new exposure beyond this bound.
const errorLogBodyBound = 512

type errorLogResponseWriter struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (writer *errorLogResponseWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *errorLogResponseWriter) Write(data []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	if writer.status >= 400 && len(writer.body) < errorLogBodyBound {
		remaining := errorLogBodyBound - len(writer.body)
		if remaining > len(data) {
			remaining = len(data)
		}
		writer.body = append(writer.body, data[:remaining]...)
	}
	return writer.ResponseWriter.Write(data)
}

func (writer *errorLogResponseWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap keeps http.ResponseController able to reach the underlying writer's
// optional interfaces (flush, deadlines) through this recorder.
func (writer *errorLogResponseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// errorLogHandler emits one bounded line per failed request so development and
// launcher-captured logs can attribute 4xx/5xx responses without transport
// captures. It logs only what the client already received.
func errorLogHandler(next http.Handler, log io.Writer) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorder := &errorLogResponseWriter{ResponseWriter: writer}
		next.ServeHTTP(recorder, request)
		if recorder.status >= 400 {
			fmt.Fprintf(
				log, "%d %s %s: %s\n",
				recorder.status, request.Method, request.URL.Path, compactErrorBody(recorder.body),
			)
		}
	})
}

func compactErrorBody(body []byte) string {
	return strings.Join(strings.Fields(string(body)), " ")
}

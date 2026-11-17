package middleware_test

import (
	"context"
	"net/http"
	"sync"

	"github.com/jun-develop/junkit/notification"
)

// fakeNotifier mocks notification.Notifier by recording sent messages for testing.
type fakeNotifier struct {
	messages chan string

	mu      sync.Mutex
	reports []notification.ErrorReport
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{messages: make(chan string, 8)}
}

func (f *fakeNotifier) Send(_ context.Context, _ notification.Level, msg string) {
	f.messages <- msg
}

func (f *fakeNotifier) ReportError(_ context.Context, _ *http.Request, report notification.ErrorReport) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports = append(f.reports, report)
}

func (f *fakeNotifier) errorReports() []notification.ErrorReport {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]notification.ErrorReport(nil), f.reports...)
}

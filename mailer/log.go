package mailer

import (
	"context"

	"go.uber.org/zap"
)

// logSender implements Sender by logging each message instead of sending it. The body is logged
// so OTP/reset links are usable locally — never use it in production.
type logSender struct {
	from   string
	logger *zap.Logger
}

// newLogSender creates a logSender; a nil logger falls back to zap.NewNop.
func newLogSender(from string, logger *zap.Logger) *logSender {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &logSender{from: from, logger: logger}
}

// Send implements Sender.
func (s *logSender) Send(_ context.Context, msg Message) error {
	from := s.from
	if from == "" {
		from = "dev <dev@localhost>"
	}

	env, err := prepare(msg, from)
	if err != nil {
		return err
	}

	body := msg.Text
	if body == "" {
		body = msg.HTML
	}

	s.logger.Info("mailer: email (log driver, not sent)",
		zap.String("from", env.from.String()),
		zap.Strings("to", append(append(append([]string{}, msg.To...), msg.Cc...), msg.Bcc...)),
		zap.String("subject", msg.Subject),
		zap.String("body", body),
		zap.Int("attachments", len(msg.Attachments)),
	)

	return nil
}

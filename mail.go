package goserver

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type EmailMessage struct {
	From    string
	To      []string
	Cc      []string
	Bcc     []string
	Subject string
	Text    string
	HTML    string
	Files   []string
	Inline  []string
}

type Mailer struct {
	bufferPool sync.Pool
	queue      chan EmailMessage
	Host       string
	User       string
	Pass       string
	From       string
	Port       int
}

type lineWrapper struct {
	w     io.Writer
	count int
}

func (s *Server) NewMailer() *Mailer {
	port, _ := strconv.Atoi(s.Config.SmtpPort)
	if port == 0 {
		port = 25
	}

	queueSize, _ := strconv.Atoi(s.Config.SmtpQueueSize)
	if queueSize <= 0 {
		queueSize = 100
	}

	workers, _ := strconv.Atoi(s.Config.SmtpWorkers)
	if workers <= 0 {
		workers = 1
	}

	m := &Mailer{
		Host: s.Config.SmtpHost,
		Port: port,
		User: s.Config.SmtpUser,
		Pass: s.Config.SmtpPassword,
		From: s.Config.SmtpFrom,

		queue: make(chan EmailMessage, queueSize),

		bufferPool: sync.Pool{
			New: func() any {
				return new(bytes.Buffer)
			},
		},
	}

	if m.Host == "" {
		log.Println("WARNING: SMTP_HOST не задан. Отправка писем работать не будет.")
	} else {
		for i := 0; i < workers; i++ {
			go m.worker()
		}
	}

	return m
}

func (m *Mailer) SendEmailAsync(msg EmailMessage) error {
	if m.Host == "" {
		return errors.New("smtp is not configured")
	}

	if msg.From == "" {
		msg.From = m.From
	}

	select {
	case m.queue <- msg:
		return nil
	default:
		return errors.New("email queue is full (high load), message dropped")
	}
}

func (m *Mailer) SendEmail(msg EmailMessage) error {
	if m.Host == "" {
		return errors.New("smtp is not configured")
	}

	if msg.From == "" {
		msg.From = m.From
	}

	return m.processEmail(msg)
}

func (m *Mailer) worker() {
	for msg := range m.queue {
		err := m.processEmail(msg)
		if err != nil {
			log.Printf("Failed to send async email to %v: %v", msg.To, err)
		}
	}
}

func (m *Mailer) processEmail(msg EmailMessage) error {
	buf := m.bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer m.bufferPool.Put(buf)

	mixedWriter := multipart.NewWriter(buf)

	headers := map[string]string{
		"From":         msg.From,
		"To":           strings.Join(msg.To, ", "),
		"Subject":      mime.QEncoding.Encode("UTF-8", msg.Subject),
		"MIME-Version": "1.0",
		"Content-Type": "multipart/mixed; boundary=" + mixedWriter.Boundary(),
	}

	if len(msg.Cc) > 0 {
		headers["Cc"] = strings.Join(msg.Cc, ", ")
	}

	for k, v := range headers {
		fmt.Fprintf(buf, "%s: %s\r\n", k, v)
	}
	buf.WriteString("\r\n")

	altWriter := multipart.NewWriter(buf)
	fmt.Fprintf(buf, "--%s\r\n", mixedWriter.Boundary())
	fmt.Fprintf(buf, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", altWriter.Boundary())

	if msg.Text != "" {
		writeTextPart(altWriter, "text/plain; charset=utf-8", msg.Text)
	}

	if msg.HTML != "" {
		relWriter := multipart.NewWriter(buf)
		fmt.Fprintf(buf, "--%s\r\n", altWriter.Boundary())
		fmt.Fprintf(buf, "Content-Type: multipart/related; boundary=%s\r\n\r\n", relWriter.Boundary())

		writeTextPart(relWriter, "text/html; charset=utf-8", msg.HTML)

		for _, file := range msg.Inline {
			if err := writeFilePart(relWriter, file, "inline"); err != nil {
				return err
			}
			err := os.Remove(file)
			if err != nil {
				log.Printf("failed to remove file %s: %v", file, err)
				return err
			}
		}
		if err := relWriter.Close(); err != nil {
			return fmt.Errorf("failed to close relWriter: %w", err)
		}
	}

	defer func() {
		_ = altWriter.Close()
	}()

	for _, file := range msg.Files {
		if err := writeFilePart(mixedWriter, file, "attachment"); err != nil {
			return err
		}
		if err := os.Remove(file); err != nil {
			log.Printf("failed to remove temporary file %s: %v", file, err)
		}
	}

	if err := mixedWriter.Close(); err != nil {
		return fmt.Errorf("failed to close mixed writer: %w", err)
	}

	addr := m.Host
	if strings.Contains(addr, ":") && !strings.HasPrefix(addr, "[") {
		addr = fmt.Sprintf("[%s]", addr)
	}
	addr = fmt.Sprintf("%s:%d", addr, m.Port)

	var c *smtp.Client
	var conn net.Conn
	var err error

	if m.Port == 465 {
		conn, err = tls.Dial("tcp", addr, &tls.Config{ServerName: m.Host})
	} else {
		conn, err = net.Dial("tcp", addr)
	}

	if err != nil {
		return fmt.Errorf("dial error: %w", err)
	}

	c, err = smtp.NewClient(conn, m.Host)
	if err != nil {
		return fmt.Errorf("smtp client error: %w", err)
	}

	if m.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: m.Host}); err != nil {
				return fmt.Errorf("starttls error: %w", err)
			}
		}
	}

	auth := smtp.PlainAuth("", m.User, m.Pass, m.Host)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("auth error: %w", err)
	}

	if err := c.Mail(m.From); err != nil {
		return err
	}

	allRecipients := append(append(msg.To, msg.Cc...), msg.Bcc...)
	for _, rcpt := range allRecipients {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}

	w, err := c.Data()
	if err != nil {
		return err
	}

	if _, err := w.Write(buf.Bytes()); err != nil {
		return err
	}

	if err := w.Close(); err != nil {
		return err
	}

	return c.Quit()
}

func writeTextPart(w *multipart.Writer, contentType, body string) {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Type", contentType)
	h.Set("Content-Transfer-Encoding", "quoted-printable")

	part, _ := w.CreatePart(h)
	qp := quotedprintable.NewWriter(part)
	if _, err := qp.Write([]byte(body)); err != nil {
		log.Printf("failed to write to qp: %v", err)
	}
	defer func() {
		_ = qp.Close()
	}()
}

func writeFilePart(w *multipart.Writer, file, disposition string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	_, fname := filepath.Split(file)
	mimeType := mime.TypeByExtension(filepath.Ext(file))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	h := make(textproto.MIMEHeader)
	h.Set("Content-Type", mimeType)
	h.Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, fname))

	if disposition == "inline" {
		h.Set("Content-ID", "<"+fname+">")
	}

	h.Set("Content-Transfer-Encoding", "base64")

	part, _ := w.CreatePart(h)
	enc := base64.NewEncoder(base64.StdEncoding, newBase64LineWriter(part))

	if _, err := enc.Write(data); err != nil {
		return err
	}

	return enc.Close()
}

func newBase64LineWriter(w io.Writer) io.Writer {
	return &lineWrapper{w: w}
}

func (lw *lineWrapper) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := 76 - lw.count
		n = min(n, len(p))

		m, err := lw.w.Write(p[:n])
		if err != nil {
			return total, err
		}

		total += m
		lw.count += m
		p = p[n:]

		if lw.count >= 76 {
			if _, err := lw.w.Write([]byte("\r\n")); err != nil {
				return total, err
			}
			lw.count = 0
		}
	}
	return total, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

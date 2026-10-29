package service

import (
	"bufio"
	"net"
	"strings"
	"testing"
)

func TestSendMailSecure_RejectsServerWithoutSTARTTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	errChan := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)

		// 1. Initial greeting
		_, _ = writer.WriteString("220 mock.smtp.test ESMTP Service Ready\r\n")
		_ = writer.Flush()

		// 2. Read EHLO/HELO
		_, err = reader.ReadString('\n')
		if err != nil {
			errChan <- err
			return
		}

		// 3. Respond without STARTTLS
		_, _ = writer.WriteString("250-mock.smtp.test\r\n250 8BITMIME\r\n")
		_ = writer.Flush()
		errChan <- nil
	}()

	addr := ln.Addr().String()
	err = sendMailSecure(addr, nil, "from@example.com", []string{"to@example.com"}, []byte("Subject: test\r\n\r\nbody"))
	if err == nil {
		t.Fatal("expected sendMailSecure to fail when server lacks STARTTLS, but it succeeded")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("expected STARTTLS error, got: %v", err)
	}

	if serverErr := <-errChan; serverErr != nil {
		t.Logf("mock server note: %v", serverErr)
	}
}

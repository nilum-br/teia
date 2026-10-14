package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

const password = "s3cret"

// fakeServer rejects every login with an error that echoes the password,
// the worst a server or proxy in between could do.
func fakeServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				backend := pgproto3.NewBackend(conn, conn)
				for {
					msg, err := backend.ReceiveStartupMessage()
					if err != nil {
						return
					}
					if _, ok := msg.(*pgproto3.StartupMessage); ok {
						break
					}
					conn.Write([]byte("N")) // no TLS, no GSS
				}
				backend.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: "28P01",
					Message: fmt.Sprintf("password authentication failed for user \"teia\" with %q", password)})
				backend.Flush()
			}()
		}
	}()
	return ln.Addr().String()
}

func TestDSNPasswordStaysOutOfErrorsAndFindings(t *testing.T) {
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := closed.Addr().String()
	closed.Close()
	rejecting := fakeServer(t)

	cases := []struct {
		name, dsn string
		finding   bool // a login finding instead of an error
	}{
		{"malformed url", "postgres://teia:" + password + "@db.example:notaport/orders", false},
		{"password with an unencoded slash", "postgres://teia:" + password + "/x@db.example/orders", false},
		{"unterminated quote", "host=db.example user=teia password='" + password, false},
		{"refused url", "postgres://teia:" + password + "@" + refused + "/orders?connect_timeout=5", false},
		{"refused key=value", "host=127.0.0.1 port=" + port(refused) + " user=teia password=" + password + " dbname=orders connect_timeout=5", false},
		{"login rejected", "postgres://teia:" + password + "@" + rejecting + "/orders?sslmode=disable&connect_timeout=5", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obs, err := New("postgres/orders-db", Config{Node: "orders-db", DSN: c.dsn}).Discover(context.Background())
			if c.finding {
				if err != nil || len(obs.Findings) != 1 {
					t.Fatalf("want a login finding, got %+v, %v", obs, err)
				}
				f := obs.Findings[0]
				if text := f.Title + f.Detail + f.Fix.Snippet + obs.Message; strings.Contains(text, password) {
					t.Errorf("finding leaks the password: %+v", f)
				}
				return
			}
			if err == nil {
				t.Fatal("want an error")
			}
			if strings.Contains(err.Error(), password) || !strings.Contains(err.Error(), "orders-db") {
				t.Errorf("error leaks the password or misses the node: %v", err)
			}
		})
	}
}

func TestRedactKeepsTheErrorChain(t *testing.T) {
	err := redact(fmt.Errorf("connecting: %w", &pgconn.PgError{Code: "28P01", Message: "bad " + password}), password)
	var pgErr *pgconn.PgError
	if strings.Contains(err.Error(), password) || !errors.As(err, &pgErr) {
		t.Errorf("redact(...) = %v, PgError found: %v", err, pgErr != nil)
	}
	plain := errors.New("connection refused")
	if redact(plain, password) != plain || redact(plain, "") != plain {
		t.Error("errors without the password should pass through untouched")
	}
}

func port(addr string) string {
	_, p, _ := net.SplitHostPort(addr)
	return p
}

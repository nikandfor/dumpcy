package main

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"nikand.dev/go/cli"
	"nikand.dev/go/graceful"
	"nikand.dev/go/hacked/hnet"
	"tlog.app/go/errors"
	"tlog.app/go/tlog"
	"tlog.app/go/tlog/ext/tlflag"
)

func main() {
	app := &cli.Command{
		Name:   "dumproxy",
		Action: run,
		Before: before,
		Flags: []*cli.Flag{
			cli.NewFlag("tcp", "", "tcp proxy (listen_addr=remote_addr)"),

			{Description: "logging flags"},
			cli.NewFlag("log", "stderr?console=dm", "log destination"),
			cli.NewFlag("v", "", "log verbosity"),
			cli.NewFlag("debug", "", "debug http address"),

			nil,
			cli.FlagfileFlag,
			cli.EnvfileFlag,
			cli.HelpFlag,
		},
	}

	cli.RunAndExit(app, os.Args, os.Environ())
}

func before(c *cli.Command) error {
	w, err := tlflag.OpenWriter(c.String("log"))
	if err != nil {
		return errors.Wrap(err, "open log writer")
	}

	tlog.DefaultLogger = tlog.New(w)
	tlog.DefaultLogger.SetVerbosity(c.String("v"))

	return nil
}

func run(c *cli.Command) (err error) {
	tr := tlog.Start("dumproxy")
	defer tr.Finish("err", &err)

	ctx := context.Background()
	ctx = tlog.ContextWithSpan(ctx, tr)

	g := graceful.New()

	for _, q := range strings.FieldsFunc(c.String("tcp"), isComma) {
		loc, rem, _ := strings.Cut(q, "=")

		if _, err := net.ResolveTCPAddr("tcp", rem); err != nil {
			return errors.Wrap(err, "tcp proxy: invalid remote address")
		}

		l, err := net.Listen("tcp", loc)
		if err != nil {
			return errors.Wrap(err, "tcp proxy: listen")
		}

		defer closer(l, &err, "close tcp listener")

		tr.Printw("listeting tcp", "addr", l.Addr(), "remote", rem)

		g.Add(func(ctx context.Context) error {
			var wg sync.WaitGroup
			defer wg.Wait()

			for {
				c, err := hnet.Accept(ctx, l)
				if err != nil {
					return errors.Wrap(err, "accept tcp")
				}

				wg.Add(1)

				go func() (err error) {
					defer wg.Done()

					return handleConn(ctx, c, rem)
				}()
			}
		})
	}

	return g.Run(ctx, graceful.IgnoreErrors(context.Canceled))
}

func handleConn(ctx context.Context, c net.Conn, remote string) (err error) {
	tr, ctx := tlog.SpawnFromContextAndWrap(ctx, "conn", "laddr", c.LocalAddr(), "raddr", c.RemoteAddr(), "remote", remote)
	defer tr.Finish("err", &err)

	defer closer(c, &err, "close client conn")
	var d net.Dialer

	r, err := d.DialContext(ctx, "tcp", remote)
	if err != nil {
		return errors.Wrap(err, "dial remote")
	}

	defer closer(r, &err, "close remote conn")

	errc := make(chan error, 2)

	go func() {
		errc <- proxy(ctx, c, r, "remote to client")
	}()
	go func() {
		errc <- proxy(ctx, r, c, "client to remote")
	}()

	err = <-errc
	err1 := <-errc

	if err == nil {
		err = err1
	}
	if err != nil {
		return errors.Wrap(err, "proxy")
	}

	return nil
}

func proxy(ctx context.Context, w, r net.Conn, name string) (err error) {
	tr := tlog.SpanFromContext(ctx)

	defer func() {
		if err != nil {
			err = errors.Wrap(err, "%v", name)
		}
	}()

	meter := MakeMeter(time.Now().UnixNano())

	defer closerFunc(w.(*net.TCPConn).CloseWrite, &err, "%v: close writer", name)

	var buf [0x4000]byte

	for {
		n, err := hnet.Read(ctx, r, buf[:])
		if errors.Is(err, io.EOF) {
			tr.Printw("eof", "proxy", name)
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "read")
		}

		tr.Printw("read", "proxy", name, "n", n, "n", tlog.NextAsHex, n, "read", buf[:n])

		m, err := w.Write(buf[:n])
		if err != nil {
			return errors.Wrap(err, "write")
		}

		now := time.Now()
		meter.Add(now.UnixNano(), m)
		bps := meter.SpeedBPS()

		tr.Printw("written", "proxy", name, "m", m, "m", tlog.NextAsHex, m, "rate_MBps", bps/1e6)
	}
}

func closer(c io.Closer, errp *error, msg string, args ...any) {
	closerFunc(c.Close, errp, msg, args...)
}

func closerFunc(c func() error, errp *error, msg string, args ...any) {
	e := c()
	if *errp == nil && e != nil {
		*errp = errors.Wrap(e, msg, args...)
	}
}

func isComma(r rune) bool { return r == ',' }

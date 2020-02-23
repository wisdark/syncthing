// Copyright (C) 2015 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package dialer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/net/proxy"
)

var errUnexpectedInterfaceType = errors.New("unexpected interface type")

// SetTCPOptions sets our default TCP options on a TCP connection, possibly
// digging through dialerConn to extract the *net.TCPConn
func SetTCPOptions(conn net.Conn) error {
	switch conn := conn.(type) {
	case *net.TCPConn:
		var err error
		if err = conn.SetLinger(0); err != nil {
			return err
		}
		if err = conn.SetNoDelay(false); err != nil {
			return err
		}
		if err = conn.SetKeepAlivePeriod(60 * time.Second); err != nil {
			return err
		}
		if err = conn.SetKeepAlive(true); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown connection type %T", conn)
	}
}

func SetTrafficClass(conn net.Conn, class int) error {
	switch conn := conn.(type) {
	case *net.TCPConn:
		e1 := ipv4.NewConn(conn).SetTOS(class)
		e2 := ipv6.NewConn(conn).SetTrafficClass(class)

		if e1 != nil {
			return e1
		}
		return e2
	default:
		return fmt.Errorf("unknown connection type %T", conn)
	}
}

func dialContextWithFallback(ctx context.Context, fallback proxy.ContextDialer, network, addr string) (net.Conn, error) {
	dialer, ok := proxy.FromEnvironment().(proxy.ContextDialer)
	if !ok {
		return nil, errUnexpectedInterfaceType
	}
	if dialer == proxy.Direct {
		conn, err := fallback.DialContext(ctx, network, addr)
		l.Debugf("Dialing direct result %s %s: %v %v", network, addr, conn, err)
		return conn, err
	}
	if noFallback {
		conn, err := dialer.DialContext(ctx, network, addr)
		l.Debugf("Dialing no fallback result %s %s: %v %v", network, addr, conn, err)
		return conn, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var proxyConn, fallbackConn net.Conn
	var proxyErr, fallbackErr error
	proxyDone := make(chan struct{})
	fallbackDone := make(chan struct{})
	go func() {
		proxyConn, proxyErr = dialer.DialContext(ctx, network, addr)
		l.Debugf("Dialing proxy result %s %s: %v %v", network, addr, proxyConn, proxyErr)
		close(proxyDone)
	}()
	go func() {
		fallbackConn, fallbackErr = fallback.DialContext(ctx, network, addr)
		l.Debugf("Dialing fallback result %s %s: %v %v", network, addr, fallbackConn, fallbackErr)
		close(fallbackDone)
	}()
	<-proxyDone
	if proxyErr == nil {
		go func() {
			<-fallbackDone
			if fallbackErr == nil {
				fallbackConn.Close()
			}
		}()
		return proxyConn, nil
	}
	<-fallbackDone
	return fallbackConn, fallbackErr
}

// DialContext dials via context and/or directly, depending on how it is configured.
// If dialing via proxy and allowing fallback, dialing for both happens simultaneously
// and the proxy connection is returned if successful.
func DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return dialContextWithFallback(ctx, proxy.Direct, network, addr)
}

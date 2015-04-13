// Copyright 2013-2015 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package sm

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fiorix/go-diameter/diam"
	"github.com/fiorix/go-diameter/diam/avp"
	"github.com/fiorix/go-diameter/diam/datatype"
	"github.com/fiorix/go-diameter/diam/diamtest"
	"github.com/fiorix/go-diameter/diam/dict"
	"github.com/fiorix/go-diameter/diam/sm/parser"
)

func TestClient_Dial_MissingStateMachine(t *testing.T) {
	cli := &Client{}
	_, err := cli.Dial("")
	if err != ErrMissingStateMachine {
		t.Fatal(err)
	}
}

func TestClient_Dial_MissingApplication(t *testing.T) {
	cli := &Client{Handler: New(&Settings{})}
	_, err := cli.Dial("")
	if err != parser.ErrMissingApplication {
		t.Fatal(err)
	}
}

func TestClient_Dial_InvalidAddress(t *testing.T) {
	cli := &Client{
		Handler: New(clientSettings),
		AcctApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0,
				datatype.Unsigned32(0)),
		},
	}
	c, err := cli.Dial(":0")
	if err == nil {
		c.Close()
		t.Fatal("Invalid client address succeeded")
	}
}

func TestClient_DialTLS_InvalidAddress(t *testing.T) {
	cli := &Client{
		Handler: New(clientSettings),
		AcctApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
		},
	}
	c, err := cli.DialTLS(":0", "", "")
	if err == nil {
		c.Close()
		t.Fatal("Invalid client address succeeded")
	}
}

func TestClient_Handshake(t *testing.T) {
	srv := diamtest.NewServer(New(serverSettings), dict.Default)
	defer srv.Close()
	cli := &Client{
		Handler: New(clientSettings),
		SupportedVendorID: []*diam.AVP{
			diam.NewAVP(avp.SupportedVendorID, avp.Mbit, 0, clientSettings.VendorID),
		},
		AcctApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
		},
		AuthApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
		},
		VendorSpecificApplicationID: []*diam.AVP{
			diam.NewAVP(avp.VendorSpecificApplicationID, avp.Mbit, 0, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
				},
			}),
		},
	}
	c, err := cli.Dial(srv.Address)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestClient_Handshake_Notify(t *testing.T) {
	srv := diamtest.NewServer(New(serverSettings), dict.Default)
	defer srv.Close()
	cli := &Client{
		Handler: New(clientSettings),
		SupportedVendorID: []*diam.AVP{
			diam.NewAVP(avp.SupportedVendorID, avp.Mbit, 0, clientSettings.VendorID),
		},
		AcctApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
		},
		AuthApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
		},
		VendorSpecificApplicationID: []*diam.AVP{
			diam.NewAVP(avp.VendorSpecificApplicationID, avp.Mbit, 0, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
				},
			}),
		},
	}
	handshakeOK := make(chan struct{})
	go func() {
		<-cli.Handler.HandshakeNotify()
		close(handshakeOK)
	}()
	c, err := cli.Dial(srv.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	select {
	case <-handshakeOK:
	case <-time.After(time.Second):
		t.Fatal("Handshake timed out")
	}
}

func TestClient_Handshake_FailParseCEA(t *testing.T) {
	mux := diam.NewServeMux()
	mux.HandleFunc("CER", func(c diam.Conn, m *diam.Message) {
		a := m.Answer(diam.Success)
		// Missing Origin-Host and other mandatory AVPs.
		a.WriteTo(c)
	})
	srv := diamtest.NewServer(mux, dict.Default)
	defer srv.Close()
	cli := &Client{
		Handler: New(clientSettings),
		AcctApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
		},
	}
	_, err := cli.Dial(srv.Address)
	if err != parser.ErrMissingOriginHost {
		t.Fatal(err)
	}
}

func TestClient_Handshake_FailedResultCode(t *testing.T) {
	mux := diam.NewServeMux()
	mux.HandleFunc("CER", func(c diam.Conn, m *diam.Message) {
		cer := new(parser.CER)
		if _, err := cer.Parse(m); err != nil {
			panic(err)
		}
		a := m.Answer(diam.NoCommonApplication)
		a.NewAVP(avp.OriginHost, avp.Mbit, 0, clientSettings.OriginHost)
		a.NewAVP(avp.OriginRealm, avp.Mbit, 0, clientSettings.OriginRealm)
		a.AddAVP(cer.OriginStateID)
		a.AddAVP(cer.AcctApplicationID[0]) // The one we send below.
		a.WriteTo(c)
	})
	srv := diamtest.NewServer(mux, dict.Default)
	defer srv.Close()
	cli := &Client{
		Handler: New(clientSettings),
		AcctApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
		},
	}
	_, err := cli.Dial(srv.Address)
	if err == nil {
		t.Fatal("Unexpected CER worked")
	}
	e, ok := err.(*ErrFailedResultCode)
	if !ok {
		t.Fatal(err)
	}
	if !strings.Contains(e.Error(), "failed Result-Code AVP") {
		t.Fatal(e.Error())
	}
}

func TestClient_Handshake_UnexpectedOriginStateID(t *testing.T) {
	mux := diam.NewServeMux()
	mux.HandleFunc("CER", func(c diam.Conn, m *diam.Message) {
		cer := new(parser.CER)
		if _, err := cer.Parse(m); err != nil {
			panic(err)
		}
		a := m.Answer(diam.Success)
		a.NewAVP(avp.OriginHost, avp.Mbit, 0, clientSettings.OriginHost)
		a.NewAVP(avp.OriginRealm, avp.Mbit, 0, clientSettings.OriginRealm)
		cer.OriginStateID.Data = datatype.Unsigned32(1) // Force fail.
		a.AddAVP(cer.OriginStateID)
		a.AddAVP(cer.AcctApplicationID[0]) // The one we send below.
		a.WriteTo(c)
	})
	srv := diamtest.NewServer(mux, dict.Default)
	defer srv.Close()
	cli := &Client{
		Handler: New(clientSettings),
		AcctApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
		},
	}
	_, err := cli.Dial(srv.Address)
	if err == nil {
		t.Fatal("Unexpected CER worked")
	}
	if err != ErrUnexpectedOriginStateID {
		t.Fatal(err)
	}
}

func TestClient_Handshake_RetransmitTimeout(t *testing.T) {
	mux := diam.NewServeMux()
	retransmits := 0
	mux.HandleFunc("CER", func(c diam.Conn, m *diam.Message) {
		// Do nothing to force timeout.
		retransmits++
	})
	srv := diamtest.NewServer(mux, dict.Default)
	defer srv.Close()
	cli := &Client{
		Handler:            New(clientSettings),
		MaxRetransmits:     3,
		RetransmitInterval: time.Millisecond,
		AcctApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
		},
	}
	_, err := cli.Dial(srv.Address)
	if err == nil {
		t.Fatal("Unexpected CER worked")
	}
	if err != ErrHandshakeTimeout {
		t.Fatal(err)
	}
	if retransmits != 4 {
		t.Fatalf("Unexpected # of retransmits. Want 4, have %d", retransmits)
	}
}

func TestClient_Watchdog(t *testing.T) {
	srv := diamtest.NewServer(New(serverSettings), dict.Default)
	defer srv.Close()
	cli := &Client{
		EnableWatchdog:   true,
		WatchdogInterval: 100 * time.Millisecond,
		Handler:          New(clientSettings),
		AcctApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
		},
	}
	c, err := cli.Dial(srv.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	resp := make(chan uint32, 1)
	dwa := handleDWA(cli.Handler, resp)
	cli.Handler.mux.HandleFunc("DWA", func(c diam.Conn, m *diam.Message) {
		dwa(c, m)
	})
	select {
	case osid := <-resp:
		if osid != 1 {
			t.Fatalf("Unexpected Origin-State-Id. Want 1, have %d", osid)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timeout waiting for DWA")
	}
}

func TestClient_Watchdog_Timeout(t *testing.T) {
	sm := New(serverSettings)
	var once sync.Once
	sm.mux.HandleFunc("DWR", func(c diam.Conn, m *diam.Message) {
		once.Do(func() { m.Answer(diam.UnableToComply).WriteTo(c) })
	})
	srv := diamtest.NewServer(sm, dict.Default)
	defer srv.Close()
	cli := &Client{
		MaxRetransmits:     3,
		RetransmitInterval: 50 * time.Millisecond,
		EnableWatchdog:     true,
		WatchdogInterval:   50 * time.Millisecond,
		Handler:            New(clientSettings),
		AcctApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AcctApplicationID, avp.Mbit, 0, datatype.Unsigned32(0)),
		},
	}
	c, err := cli.Dial(srv.Address)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	select {
	case <-c.(diam.CloseNotifier).CloseNotify():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for watchdog to disconnect client")
	}
}
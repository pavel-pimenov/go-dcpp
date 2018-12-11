package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dennwc/go-dcpp/adc"
)

var (
	ErrPeerOffline = errors.New("peer is offline")
	ErrPeerPassive = errors.New("peer is passive")
)

func (p *Peer) CanDial() bool {
	// online and active
	return p.Online() && p.Info().Features.Has("TCP4")
}

// Dial tries to dial the peer either in passive or active mode.
func (p *Peer) Dial(ctx context.Context) (*PeerConn, error) {
	if !p.Online() {
		return nil, ErrPeerOffline
	}
	// TODO: active mode
	return p.dialPassive(ctx)
}

func (p *Peer) dialPassive(ctx context.Context) (*PeerConn, error) {
	if !p.Info().Features.Has("TCP4") {
		return nil, ErrPeerPassive
	}
	sid := p.getSID()
	if sid == nil {
		return nil, ErrPeerOffline
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	token, caddr, errc := p.hub.revConnToken(ctx, p.Info().Id)
	data, err := adc.Marshal(adc.RCMParams{
		Proto: adc.ProtoADC, Token: token,
	})
	if err != nil {
		return nil, err
	}

	err = p.hub.writeCommand(&adc.DirectCmd{
		Name: adc.CmdRevConnectToMe,
		Targ: *sid, Raw: data,
	})
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err = <-errc:
		return nil, err
	case addr := <-caddr:
		pconn, err := adc.Dial(addr)
		if err != nil {
			return nil, err
		}
		fea, err := p.handshakePassive(pconn, token)
		if err != nil {
			pconn.Close()
			return nil, err
		}
		return &PeerConn{p: p, conn: pconn, fea: fea}, nil
	}
}

func (p *Peer) handshakePassive(conn *adc.Conn, token string) (adc.ModFeatures, error) {
	// we are dialing - send things upfront

	// send our features
	ourFeatures := adc.ModFeatures{
		// should always be set for ADC
		adc.FeaBASE: true,
		adc.FeaTIGR: true,
	}
	data, err := adc.Marshal(ourFeatures)
	if err != nil {
		return nil, err
	}
	err = conn.WriteCmd(adc.ClientCmd{
		Name: adc.CmdSupport, Raw: data,
	})

	// send an identification as well
	data, err = adc.Marshal(adc.User{Id: p.hub.CID(), Token: token})
	if err != nil {
		return nil, err
	}
	err = conn.WriteCmd(adc.ClientCmd{
		Name: adc.CmdInfo, Raw: data,
	})

	// flush both
	err = conn.Flush()
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(time.Second * 5)
	// wait for a list of features
	cmd, err := conn.ReadCmd(deadline)
	if err != nil {
		return nil, err
	}
	cc, ok := cmd.(adc.ClientCmd)
	if !ok {
		return nil, fmt.Errorf("expected client command, got: %#v", cmd)
	} else if cc.Name != adc.CmdSupport {
		return nil, fmt.Errorf("expected a list of peer's features, got: %#v", cmd)
	}

	var peerFeatures adc.ModFeatures
	if err := peerFeatures.UnmarshalAdc(cc.Raw); err != nil {
		return nil, err
	} else if !peerFeatures.IsSet(adc.FeaBASE) || !peerFeatures.IsSet(adc.FeaTIGR) {
		return nil, fmt.Errorf("no basic features support for peer: %v", peerFeatures)
	}

	// wait for an identification
	cmd, err = conn.ReadCmd(deadline)
	if err != nil {
		return nil, err
	}
	cc, ok = cmd.(adc.ClientCmd)
	if !ok {
		return nil, fmt.Errorf("expected client command, got: %#v", cmd)
	} else if cc.Name != adc.CmdInfo {
		return nil, fmt.Errorf("expected a peer's identity, got: %#v", cmd)
	}

	var u adc.User
	if err := adc.Unmarshal(cc.Raw, &u); err != nil {
		return nil, err
	} else if u.Id != p.Info().Id {
		return nil, fmt.Errorf("wrong client connected: %v", u.Id)
	}
	return ourFeatures.Intersect(peerFeatures), nil
}

func (p *Peer) handshakeActive(conn *adc.Conn, token string) (adc.ModFeatures, error) {
	// we are accepting the connection, so wait for a message from peer
	deadline := time.Now().Add(time.Second * 5)

	// wait for a list of features
	cmd, err := conn.ReadCmd(deadline)
	if err != nil {
		return nil, err
	}
	cc, ok := cmd.(adc.ClientCmd)
	if !ok {
		return nil, fmt.Errorf("expected client command, got: %#v", cmd)
	} else if cc.Name != adc.CmdSupport {
		return nil, fmt.Errorf("expected a list of peer's features, got: %#v", cmd)
	}

	var peerFeatures adc.ModFeatures
	if err := peerFeatures.UnmarshalAdc(cc.Raw); err != nil {
		return nil, err
	} else if !peerFeatures.IsSet(adc.FeaBASE) || !peerFeatures.IsSet(adc.FeaTIGR) {
		return nil, fmt.Errorf("no basic features support for peer: %v", peerFeatures)
	}

	// send our features
	ourFeatures := adc.ModFeatures{
		// should always be set for ADC
		adc.FeaBASE: true,
		adc.FeaTIGR: true,
	}
	data, err := adc.Marshal(ourFeatures)
	if err != nil {
		return nil, err
	}
	err = conn.WriteCmd(adc.ClientCmd{
		Name: adc.CmdSupport, Raw: data,
	})
	if err != nil {
		return nil, err
	}
	err = conn.Flush()
	if err != nil {
		return nil, err
	}

	// wait for an identification
	cmd, err = conn.ReadCmd(deadline)
	if err != nil {
		return nil, err
	}
	cc, ok = cmd.(adc.ClientCmd)
	if !ok {
		return nil, fmt.Errorf("expected client command, got: %#v", cmd)
	} else if cc.Name != adc.CmdInfo {
		return nil, fmt.Errorf("expected a peer's identity, got: %#v", cmd)
	}

	var u adc.User
	if err := adc.Unmarshal(cc.Raw, &u); err != nil {
		return nil, err
	} else if u.Id != p.Info().Id {
		return nil, fmt.Errorf("wrong client connected: %v", u.Id)
	} else if u.Token != token {
		return nil, errors.New("wrong auth token")
	}

	// identify ourselves
	data, err = adc.Marshal(adc.User{Id: p.hub.CID()})
	if err != nil {
		return nil, err
	}
	err = conn.WriteCmd(adc.ClientCmd{
		Name: adc.CmdInfo, Raw: data,
	})
	if err != nil {
		return nil, err
	}
	err = conn.Flush()
	if err != nil {
		return nil, err
	}

	return ourFeatures.Intersect(peerFeatures), nil
}

type PeerConn struct {
	p    *Peer
	conn *adc.Conn
	fea  adc.ModFeatures
}

func (c *PeerConn) Close() error {
	return c.conn.Close()
}

func (c *PeerConn) Peer() *Peer {
	return c.p
}

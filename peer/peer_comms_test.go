package peer

import (
	"crypto/rand"
	"github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/box"
	"testing"
	"time"
)

func makePeerPair() (peer1 *LocalPeer, peer2 *LocalPeer) {
	if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
		if localPeer, err := CreatePeer(
			uuid.NewV4().String(),
			[]byte(publicKey[:]),
			[]byte(privateKey[:]),
		); err == nil {
			peer1 = localPeer
		} else {
			panic(err)
		}
	} else {
		panic(err)
	}

	if publicKey, privateKey, err := box.GenerateKey(rand.Reader); err == nil {
		if localPeer, err := CreatePeer(
			uuid.NewV4().String(),
			[]byte(publicKey[:]),
			[]byte(privateKey[:]),
		); err == nil {
			peer2 = localPeer
		} else {
			panic(err)
		}
	} else {
		panic(err)
	}

	go func() {
		if err := peer1.Listen(); err != nil {
			panic(err)
		}
	}()

	go func() {
		if err := peer2.Listen(); err != nil {
			panic(err)
		}
	}()

	// waits for both peers to declare that they're ready and listening
	for i := 0; i < 2; i++ {
		select {
		case <-peer1.WaitListen():
			continue
		case <-peer2.WaitListen():
			continue
		}
	}

	return
}

func TestPeerConnect(t *testing.T) {
	assert := require.New(t)
	peer1messages := make([]PeerMessage, 0)
	peer2messages := make([]PeerMessage, 0)

	var err error
	peer1, peer2 := makePeerPair()

	fromPeer2, err := peer1.ConnectTo(peer2.Address, peer2.Port)
	assert.Nil(err)

	fromPeer1, err := peer2.ConnectTo(peer1.Address, peer1.Port)
	assert.Nil(err)

	go func() {
		for m := range peer1.Messages {
			log.Debugf("peer1: msg: %+v", m)
			peer1messages = append(peer1messages, m)
		}
	}()

	go func() {
		for m := range peer2.Messages {
			log.Debugf("peer2: msg: %+v", m)
			peer2messages = append(peer2messages, m)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	fromPeer2.SendMessage(CommandType, []byte{0x42})
	fromPeer1.SendMessage(CommandType, []byte{0x41})

	time.Sleep(100 * time.Millisecond)

	assert.Equal(1, len(peer1messages))
	assert.Equal(fromPeer2.ID(), peer1messages[0].Peer.ID())
	assert.Equal([]byte{0x41}, peer1messages[0].Message.Data)

	assert.Equal(1, len(peer2messages))
	assert.Equal(fromPeer1.ID(), peer2messages[0].Peer.ID())
	assert.Equal([]byte{0x42}, peer2messages[0].Message.Data)
}

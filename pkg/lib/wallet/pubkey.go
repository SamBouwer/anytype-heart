package wallet

import (
	"fmt"

	"github.com/anyproto/anytype-heart/pkg/lib/strkey"
	"github.com/libp2p/go-libp2p/core/crypto"
	crypto_pb "github.com/libp2p/go-libp2p/core/crypto/pb"
	"github.com/libp2p/go-libp2p/core/peer"
)

type PubKey interface {
	Address() string
	KeypairType() KeypairType

	crypto.PubKey
}

type pubKey struct {
	keyType KeypairType
	crypto.PubKey
}

func NewPubKeyFromAddress(t KeypairType, address string) (PubKey, error) {
	if t != KeypairTypeAccount && t != KeypairTypeDevice {
		return nil, fmt.Errorf("incorrect KeypairType")
	}

	if t == KeypairTypeAccount {
		pubKeyRaw, err := strkey.Decode(accountAddressVersionByte, address)
		if err != nil {
			return nil, err
		}

		unmarshal := crypto.PubKeyUnmarshallers[crypto_pb.KeyType_Ed25519]
		pk, err := unmarshal(pubKeyRaw)
		if err != nil {
			return nil, err
		}

		return &pubKey{
			keyType: t,
			PubKey:  pk,
		}, nil
	} else {
		peerID, err := peer.Decode(address)
		if err != nil {
			return nil, err
		}

		pk, err := peerID.ExtractPublicKey()
		if err != nil {
			return nil, err
		}

		return &pubKey{
			keyType: t,
			PubKey:  pk,
		}, nil
	}
}

func (pk pubKey) Address() string {
	address, err := getAddress(pk.keyType, pk.PubKey)
	if err != nil {
		// shouldn't be a case because we check it on init
		log.Error(err)
	}

	return address
}

func (pk pubKey) KeypairType() KeypairType {
	return pk.keyType
}

func getAddress(keyType KeypairType, key crypto.PubKey) (string, error) {

	if keyType == KeypairTypeAccount {
		b, err := key.Raw()
		if err != nil {
			return "", err
		}

		return strkey.Encode(accountAddressVersionByte, b)
	} else {
		peerId, err := getPeer(keyType, key)
		if err != nil {
			return "", err
		}
		return peerId.String(), nil
	}

}

func getPeer(keyType KeypairType, key crypto.PubKey) (peer.ID, error) {
	if keyType == KeypairTypeAccount {
		return peer.ID(""), fmt.Errorf("unsupported")
	} else {
		peerId, err := peer.IDFromPublicKey(key)
		if err != nil {
			return "", err
		}

		return peerId, nil
	}
}

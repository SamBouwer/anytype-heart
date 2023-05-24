package core

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/anyproto/anytype-heart/core/session"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/wallet"
)

const wordCount int = 12

func (mw *Middleware) WalletCreate(cctx context.Context, req *pb.RpcWalletCreateRequest) *pb.RpcWalletCreateResponse {
	response := func(mnemonic string, code pb.RpcWalletCreateResponseErrorCode, err error) *pb.RpcWalletCreateResponse {
		m := &pb.RpcWalletCreateResponse{Mnemonic: mnemonic, Error: &pb.RpcWalletCreateResponseError{Code: code}}
		if err != nil {
			m.Error.Description = err.Error()
		}

		return m
	}

	mw.m.Lock()
	defer mw.m.Unlock()

	mw.rootPath = req.RootPath
	mw.foundAccounts = nil

	err := os.MkdirAll(mw.rootPath, 0700)
	if err != nil {
		return response("", pb.RpcWalletCreateResponseError_FAILED_TO_CREATE_LOCAL_REPO, err)
	}

	mnemonic, err := core.WalletGenerateMnemonic(wordCount)
	if err != nil {
		return response("", pb.RpcWalletCreateResponseError_UNKNOWN_ERROR, err)
	}

	if err = mw.setMnemonic(mnemonic); err != nil {
		return response("", pb.RpcWalletCreateResponseError_UNKNOWN_ERROR, fmt.Errorf("set mnemonic: %w", err))
	}

	return response(mnemonic, pb.RpcWalletCreateResponseError_NULL, nil)
}

func (mw *Middleware) setMnemonic(mnemonic string) error {
	mw.mnemonic = mnemonic
	acc, err := core.WalletAccountAt(mw.mnemonic, 0, "")
	if err != nil {
		return fmt.Errorf("derive private key: %w", err)
	}
	priv, err := acc.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}

	mw.privateKey = priv
	return nil
}

func (mw *Middleware) WalletRecover(cctx context.Context, req *pb.RpcWalletRecoverRequest) *pb.RpcWalletRecoverResponse {
	response := func(code pb.RpcWalletRecoverResponseErrorCode, err error) *pb.RpcWalletRecoverResponse {
		m := &pb.RpcWalletRecoverResponse{Error: &pb.RpcWalletRecoverResponseError{Code: code}}
		if err != nil {
			m.Error.Description = err.Error()
		}

		return m
	}

	mw.accountSearchCancel()

	mw.m.Lock()
	defer mw.m.Unlock()

	if mw.mnemonic == req.Mnemonic {
		return response(pb.RpcWalletRecoverResponseError_NULL, nil)
	}

	// test if mnemonic is correct
	_, err := core.WalletAccountAt(req.Mnemonic, 0, "")
	if err != nil {
		return response(pb.RpcWalletRecoverResponseError_BAD_INPUT, err)
	}

	err = os.MkdirAll(req.RootPath, 0700)
	if err != nil {
		return response(pb.RpcWalletRecoverResponseError_FAILED_TO_CREATE_LOCAL_REPO, err)
	}

	if err = mw.setMnemonic(req.Mnemonic); err != nil {
		return response(pb.RpcWalletRecoverResponseError_UNKNOWN_ERROR, err)
	}
	mw.rootPath = req.RootPath
	mw.foundAccounts = nil

	return response(pb.RpcWalletRecoverResponseError_NULL, nil)
}

func (mw *Middleware) WalletConvert(cctx context.Context, req *pb.RpcWalletConvertRequest) *pb.RpcWalletConvertResponse {
	response := func(mnemonic, entropy string, code pb.RpcWalletConvertResponseErrorCode, err error) *pb.RpcWalletConvertResponse {
		m := &pb.RpcWalletConvertResponse{Mnemonic: mnemonic, Entropy: entropy, Error: &pb.RpcWalletConvertResponseError{Code: code}}
		if err != nil {
			m.Error.Description = err.Error()
		}

		return m
	}

	if req.Mnemonic == "" && req.Entropy != "" {
		b, err := base64.RawStdEncoding.DecodeString(req.Entropy)
		if err != nil {
			return response("", "", pb.RpcWalletConvertResponseError_BAD_INPUT, fmt.Errorf("invalid base64 format for entropy: %w", err))
		}

		w, err := wallet.WalletFromEntropy(b)
		if err != nil {
			return response("", "", pb.RpcWalletConvertResponseError_BAD_INPUT, fmt.Errorf("invalid entropy: %w", err))
		}
		return response(w.RecoveryPhrase, "", pb.RpcWalletConvertResponseError_NULL, nil)
	} else if req.Entropy == "" && req.Mnemonic != "" {
		w := wallet.WalletFromMnemonic(req.Mnemonic)
		entropy, err := w.Entropy()
		if err != nil {
			return response("", "", pb.RpcWalletConvertResponseError_BAD_INPUT, err)
		}

		base64Entropy := base64.RawStdEncoding.EncodeToString(entropy)
		return response("", base64Entropy, pb.RpcWalletConvertResponseError_NULL, nil)
	}

	return response("", "", pb.RpcWalletConvertResponseError_BAD_INPUT, fmt.Errorf("you should specify neither entropy or mnemonic to convert"))
}

func (mw *Middleware) WalletCreateSession(cctx context.Context, req *pb.RpcWalletCreateSessionRequest) *pb.RpcWalletCreateSessionResponse {
	response := func(token string, code pb.RpcWalletCreateSessionResponseErrorCode, err error) *pb.RpcWalletCreateSessionResponse {
		m := &pb.RpcWalletCreateSessionResponse{Token: token, Error: &pb.RpcWalletCreateSessionResponseError{Code: code}}
		if err != nil {
			m.Error.Description = err.Error()
		}

		return m
	}

	// test if mnemonic is correct
	_, err := core.WalletAccountAt(req.Mnemonic, 0, "")
	if err != nil {
		return response("", pb.RpcWalletCreateSessionResponseError_BAD_INPUT, err)
	}

	tok, err := mw.sessions.StartSession(mw.privateKey)
	if err != nil {
		return response("", pb.RpcWalletCreateSessionResponseError_UNKNOWN_ERROR, err)
	}

	return response(tok, pb.RpcWalletCreateSessionResponseError_NULL, nil)
}

func (mw *Middleware) WalletCloseSession(cctx context.Context, req *pb.RpcWalletCloseSessionRequest) *pb.RpcWalletCloseSessionResponse {
	response := func(code pb.RpcWalletCloseSessionResponseErrorCode, err error) *pb.RpcWalletCloseSessionResponse {
		m := &pb.RpcWalletCloseSessionResponse{Error: &pb.RpcWalletCloseSessionResponseError{Code: code}}
		if err != nil {
			m.Error.Description = err.Error()
		}

		return m
	}

	if sender, ok := mw.EventSender.(session.Closer); ok {
		sender.CloseSession(req.Token)
	}
	if err := mw.sessions.CloseSession(req.Token); err != nil {
		response(pb.RpcWalletCloseSessionResponseError_UNKNOWN_ERROR, fmt.Errorf("session service: %w", err))
	}

	return response(pb.RpcWalletCloseSessionResponseError_NULL, nil)
}

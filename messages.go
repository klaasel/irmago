package irmago

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/go-errors/errors"
)

// Status encodes the status of an IRMA session (e.g., connected).
type Status string

// Version encodes the IRMA protocol version of an IRMA session.
type Version string

// Action encodes the session type of an IRMA session (e.g., disclosing).
type Action string

// ErrorType are session errors.
type ErrorType string

// SessionError is a protocol error.
type SessionError struct {
	Err error
	ErrorType
	*ApiError
	Info   string
	Status int
}

// ApiError is an error message returned by the API server on errors.
type ApiError struct {
	Status      int    `json:"status"`
	ErrorName   string `json:"error"`
	Description string `json:"description"`
	Message     string `json:"message"`
	Stacktrace  string `json:"stacktrace"`
}

// Qr contains the data of an IRMA session QR (as generated by irma_js),
// suitable for NewSession().
type Qr struct {
	// Server with which to perform the session
	URL string `json:"u"`
	// Session type (disclosing, signing, issuing)
	Type               Action `json:"irmaqr"`
	ProtocolVersion    string `json:"v"`
	ProtocolMaxVersion string `json:"vmax"`
}

// A SessionInfo is the first message in the IRMA protocol (i.e., GET on the server URL),
// containing the session request info.
type SessionInfo struct {
	Jwt     string                   `json:"jwt"`
	Nonce   *big.Int                 `json:"nonce"`
	Context *big.Int                 `json:"context"`
	Keys    map[IssuerIdentifier]int `json:"keys"`
}

// Statuses
const (
	StatusConnected     = Status("connected")
	StatusCommunicating = Status("communicating")
)

// Actions
const (
	ActionDisclosing = Action("disclosing")
	ActionSigning    = Action("signing")
	ActionIssuing    = Action("issuing")
	ActionUnknown    = Action("unknown")
)

// Protocol errors
const (
	// Protocol version not supported
	ErrorProtocolVersionNotSupported = ErrorType("protocolVersionNotSupported")
	// Error in HTTP communication
	ErrorTransport = ErrorType("transport")
	// Invalid client JWT in first IRMA message
	ErrorInvalidJWT = ErrorType("invalidJwt")
	// Unkown session type (not disclosing, signing, or issuing)
	ErrorUnknownAction = ErrorType("unknownAction")
	// Crypto error during calculation of our response (second IRMA message)
	ErrorCrypto = ErrorType("crypto")
	// Server rejected our response (second IRMA message)
	ErrorRejected = ErrorType("rejected")
	// (De)serializing of a message failed
	ErrorSerialization = ErrorType("serialization")
	// Error in keyshare protocol
	ErrorKeyshare = ErrorType("keyshare")
	// Keyshare server has blocked us
	ErrorKeyshareBlocked = ErrorType("keyshareBlocked")
	// API server error
	ErrorApi = ErrorType("api")
	// Server returned unexpected or malformed response
	ErrorServerResponse = ErrorType("serverResponse")
)

func (e *SessionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s", string(e.ErrorType), e.Err.Error())
	}
	return string(e.ErrorType)
}

func jwtDecode(jwt string, body interface{}) (string, error) {
	jwtparts := strings.Split(jwt, ".")
	if jwtparts == nil || len(jwtparts) < 2 {
		return "", errors.New("Not a JWT")
	}
	headerbytes, err := base64.RawStdEncoding.DecodeString(jwtparts[0])
	if err != nil {
		return "", err
	}
	var header struct {
		Issuer string `json:"iss"`
	}
	err = json.Unmarshal([]byte(headerbytes), &header)
	if err != nil {
		return "", err
	}

	bodybytes, err := base64.RawStdEncoding.DecodeString(jwtparts[1])
	if err != nil {
		return "", err
	}
	return header.Issuer, json.Unmarshal(bodybytes, body)
}

func parseRequestorJwt(action Action, jwt string) (RequestorJwt, string, error) {
	var retval RequestorJwt
	switch action {
	case ActionDisclosing:
		retval = &ServiceProviderJwt{}
	case ActionSigning:
		retval = &SignatureRequestorJwt{}
	case ActionIssuing:
		retval = &IdentityProviderJwt{}
	default:
		return nil, "", errors.New("Invalid session type")
	}
	server, err := jwtDecode(jwt, retval)
	if err != nil {
		return nil, "", err
	}
	return retval, server, nil
}

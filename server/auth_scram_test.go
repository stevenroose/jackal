/*
 * Copyright (c) 2018 Miguel Ángel Ortuño.
 * See the LICENSE file for more information.
 */

package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash"
	"strconv"
	"strings"
	"testing"

	"github.com/ortuman/jackal/server/transport"
	"github.com/ortuman/jackal/storage/model"
	"github.com/ortuman/jackal/util"
	"github.com/ortuman/jackal/xml"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/pbkdf2"
)

type scramAuthTestCase struct {
	id          int
	scramType   scramType
	usesCb      bool
	cbBytes     []byte
	gs2BindFlag string
	authID      string
	n           string
	r           string
	password    string
	expectedErr error
}

type scramAuthResult struct {
	clientFinalMessage string
	v                  string
}

var tt = []scramAuthTestCase{

	// Success cases
	{
		// SCRAM-SHA-1
		id:          1,
		scramType:   sha1ScramType,
		usesCb:      false,
		gs2BindFlag: "n",
		n:           "ortuman",
		r:           "bb769406-eaa4-4f38-a279-2b90e596f6dd",
		password:    "1234",
	},
	{
		id:          2,
		scramType:   sha256ScramType, // SCRAM-SHA-256
		usesCb:      false,
		gs2BindFlag: "n",
		n:           "ortuman",
		r:           "6d805d99-6dc3-4e5a-9a68-653856fc5129",
		password:    "1234",
	},
	{
		// SCRAM-SHA-1-PLUS
		id:          3,
		scramType:   sha1ScramType,
		usesCb:      true,
		cbBytes:     util.RandomBytes(23),
		gs2BindFlag: "p=tls-unique",
		authID:      "a=jackal.im",
		n:           "ortuman",
		r:           "7e51aff7-6875-4dce-820a-6d4970635006",
		password:    "1234",
	},
	{
		// SCRAM-SHA-256-PLUS
		id:          4,
		scramType:   sha256ScramType,
		usesCb:      true,
		cbBytes:     util.RandomBytes(32),
		gs2BindFlag: "p=tls-unique",
		authID:      "a=jackal.im",
		n:           "ortuman",
		r:           "d712875c-bd3b-4b41-801d-eb9c541d9884",
		password:    "1234",
	},

	// Fail cases
	{
		// invalid user
		id:          5,
		scramType:   sha1ScramType,
		usesCb:      false,
		gs2BindFlag: "n",
		n:           "mariana",
		r:           "bb769406-eaa4-4f38-a279-2b90e596f6dd",
		password:    "1234",
		expectedErr: errSASLNotAuthorized,
	},
	{
		// invalid password
		id:          6,
		scramType:   sha1ScramType,
		usesCb:      false,
		gs2BindFlag: "n",
		n:           "ortuman",
		r:           "bb769406-eaa4-4f38-a279-2b90e596f6dd",
		password:    "12345678",
		expectedErr: errSASLNotAuthorized,
	},
	{
		// not authorized gs2BindFlag
		id:          7,
		scramType:   sha1ScramType,
		usesCb:      false,
		gs2BindFlag: "y",
		n:           "ortuman",
		r:           "bb769406-eaa4-4f38-a279-2b90e596f6dd",
		password:    "1234",
		expectedErr: errSASLNotAuthorized,
	},
	{
		// invalid authID
		id:          8,
		scramType:   sha1ScramType,
		usesCb:      false,
		gs2BindFlag: "n",
		authID:      "b=jackal.im",
		n:           "ortuman",
		r:           "bb769406-eaa4-4f38-a279-2b90e596f6dd",
		password:    "1234",
		expectedErr: errSASLMalformedRequest,
	},
	{
		// not matching gs2BindFlag
		id:          9,
		scramType:   sha1ScramType,
		usesCb:      false,
		gs2BindFlag: "p=tls-unique",
		authID:      "a=jackal.im",
		n:           "ortuman",
		r:           "bb769406-eaa4-4f38-a279-2b90e596f6dd",
		password:    "1234",
		expectedErr: errSASLNotAuthorized,
	},
	{
		// not matching gs2BindFlag
		id:          10,
		scramType:   sha1ScramType,
		usesCb:      false,
		gs2BindFlag: "q=tls-unique",
		authID:      "a=jackal.im",
		n:           "ortuman",
		r:           "bb769406-eaa4-4f38-a279-2b90e596f6dd",
		password:    "1234",
		expectedErr: errSASLMalformedRequest,
	},
	{
		// empty username
		id:          10,
		scramType:   sha1ScramType,
		usesCb:      false,
		gs2BindFlag: "n",
		authID:      "a=jackal.im",
		n:           "",
		r:           "bb769406-eaa4-4f38-a279-2b90e596f6dd",
		password:    "1234",
		expectedErr: errSASLMalformedRequest,
	},
}

func TestScramMechanisms(t *testing.T) {
	testTr := transport.NewMockTransport()
	testStrm := authTestSetup(&model.User{Username: "ortuman", Password: "1234"})
	defer authTestTeardown()

	authr := newScram(testStrm, testTr, sha1ScramType, false)
	require.Equal(t, authr.Mechanism(), "SCRAM-SHA-1")
	require.False(t, authr.UsesChannelBinding())

	authr2 := newScram(testStrm, testTr, sha1ScramType, true)
	require.Equal(t, authr2.Mechanism(), "SCRAM-SHA-1-PLUS")
	require.True(t, authr2.UsesChannelBinding())

	authr3 := newScram(testStrm, testTr, sha256ScramType, false)
	require.Equal(t, authr3.Mechanism(), "SCRAM-SHA-256")
	require.False(t, authr3.UsesChannelBinding())

	authr4 := newScram(testStrm, testTr, sha256ScramType, true)
	require.Equal(t, authr4.Mechanism(), "SCRAM-SHA-256-PLUS")
	require.True(t, authr4.UsesChannelBinding())

	authr5 := newScram(testStrm, testTr, scramType(99), true)
	require.Equal(t, authr5.Mechanism(), "")
}

func TestScramBadPayload(t *testing.T) {
	testTr := transport.NewMockTransport()
	testStrm := authTestSetup(&model.User{Username: "ortuman", Password: "1234"})
	defer authTestTeardown()

	authr := newScram(testStrm, testTr, sha1ScramType, false)

	auth := xml.NewElementNamespace("auth", "urn:ietf:params:xml:ns:xmpp-sasl")
	auth.SetAttribute("mechanism", authr.Mechanism())

	// empty auth payload
	require.Equal(t, errSASLIncorrectEncoding, authr.ProcessElement(auth))

	// incorrect auth payload encoding
	authr.Reset()
	auth.SetText(".")
	require.Equal(t, errSASLIncorrectEncoding, authr.ProcessElement(auth))
}

func TestScramSuccessTestCases(t *testing.T) {
	for _, tc := range tt {
		err := processScramTestCase(t, &tc)
		if err != nil {
			require.Equal(t, tc.expectedErr, err, fmt.Sprintf("TC identifier: %d", tc.id))
			continue
		}
	}
}

func processScramTestCase(t *testing.T, tc *scramAuthTestCase) error {
	tr := transport.NewMockTransport()
	if tc.usesCb {
		tr.SetChannelBindingBytes(tc.cbBytes)
	}
	testStrm := authTestSetup(&model.User{Username: "ortuman", Password: "1234"})
	defer authTestTeardown()

	authr := newScram(testStrm, tr, tc.scramType, tc.usesCb)

	auth := xml.NewElementNamespace("auth", saslNamespace)
	auth.SetAttribute("mechanism", authr.Mechanism())

	clientInitialMessage := fmt.Sprintf(`n=%s,r=%s`, tc.n, tc.r)
	gs2Header := fmt.Sprintf(`%s,%s,`, tc.gs2BindFlag, tc.authID)
	authPayload := gs2Header + clientInitialMessage
	auth.SetText(base64.StdEncoding.EncodeToString([]byte(authPayload)))

	err := authr.ProcessElement(auth)
	if err != nil {
		return err
	}
	challenge := testStrm.FetchElement()
	require.NotNil(t, challenge)
	require.Equal(t, "challenge", challenge.Name())

	srvInitialMessage, err := base64.StdEncoding.DecodeString(challenge.Text())
	require.Nil(t, err)
	resp, err := parseScramResponse(challenge.Text())
	require.Nil(t, err)

	srvNonce := resp["r"]
	salt, err := base64.StdEncoding.DecodeString(resp["s"])
	require.Nil(t, err)

	iterations, _ := strconv.Atoi(resp["i"])

	buf := new(bytes.Buffer)
	buf.Write([]byte(gs2Header))
	if tc.usesCb {
		buf.Write(tc.cbBytes)
	}
	cBytes := base64.StdEncoding.EncodeToString(buf.Bytes())

	res := computeScramAuthResult(tc.scramType, clientInitialMessage, string(srvInitialMessage), srvNonce, cBytes, tc.password, salt, iterations)

	response := xml.NewElementNamespace("response", saslNamespace)
	response.SetText(base64.StdEncoding.EncodeToString([]byte(res.clientFinalMessage)))

	err = authr.ProcessElement(response)
	if err != nil {
		return err
	}

	success := testStrm.FetchElement()
	require.Equal(t, "success", success.Name())

	vb64, err := base64.StdEncoding.DecodeString(success.Text())
	require.Nil(t, err)
	require.Equal(t, res.v, string(vb64))

	require.True(t, authr.Authenticated())
	require.Equal(t, tc.n, authr.Username())

	require.Nil(t, authr.ProcessElement(auth)) // test already authenticated...
	return nil
}

func computeScramAuthResult(scramType scramType, clientInitialMessage, serverInitialMessage, srvNonce, cBytes, password string, salt []byte, iterations int) *scramAuthResult {
	clientFinalMessageBare := fmt.Sprintf("c=%s,r=%s", cBytes, srvNonce)

	saltedPassword := testScramAuthPbkdf2([]byte(password), salt, scramType, iterations)
	clientKey := testScramAuthHmac([]byte("Client Key"), saltedPassword, scramType)
	storedKey := testScramAuthHash(clientKey, scramType)
	authMessage := clientInitialMessage + "," + serverInitialMessage + "," + clientFinalMessageBare
	clientSignature := testScramAuthHmac([]byte(authMessage), storedKey, scramType)

	clientProof := make([]byte, len(clientKey))
	for i := 0; i < len(clientKey); i++ {
		clientProof[i] = clientKey[i] ^ clientSignature[i]
	}
	serverKey := testScramAuthHmac([]byte("Server Key"), saltedPassword, scramType)
	serverSignature := testScramAuthHmac([]byte(authMessage), serverKey, scramType)

	res := &scramAuthResult{}
	res.clientFinalMessage = clientFinalMessageBare + ",p=" + base64.StdEncoding.EncodeToString(clientProof)
	res.v = "v=" + base64.StdEncoding.EncodeToString(serverSignature)
	return res
}

func parseScramResponse(b64 string) (map[string]string, error) {
	s, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	ret := map[string]string{}
	s1 := strings.Split(string(s), ",")
	for _, s0 := range s1 {
		k, v := util.SplitKeyAndValue(s0, '=')
		ret[k] = v
	}
	return ret, nil
}

func testScramAuthPbkdf2(b []byte, salt []byte, scramType scramType, iterationCount int) []byte {
	switch scramType {
	case sha1ScramType:
		return pbkdf2.Key(b, salt, iterationCount, sha1.Size, sha1.New)
	case sha256ScramType:
		return pbkdf2.Key(b, salt, iterationCount, sha256.Size, sha256.New)
	}
	return nil
}

func testScramAuthHmac(b []byte, key []byte, scramType scramType) []byte {
	var h func() hash.Hash
	switch scramType {
	case sha1ScramType:
		h = sha1.New
	case sha256ScramType:
		h = sha256.New
	}
	m := hmac.New(h, key)
	m.Write(b)
	return m.Sum(nil)
}

func testScramAuthHash(b []byte, scramType scramType) []byte {
	var h hash.Hash
	switch scramType {
	case sha1ScramType:
		h = sha1.New()
	case sha256ScramType:
		h = sha256.New()
	}
	h.Write(b)
	return h.Sum(nil)
}

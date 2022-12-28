package ftl

import "errors"

var ErrClosed = errors.New("connection is closed")
var ErrRead = errors.New("error during read")
var ErrWrite = errors.New("error during write")
var ErrUnexpectedArguments = errors.New("unexpected arguments")

// Connection Errors
var ErrConnectBeforeAuth = errors.New("control connection attempted command before successful authentication")
var ErrMultipleConnect = errors.New("control connection attempted multiple CONNECT handshakes")
var ErrInvalidHmacHash = errors.New("client provided invalid HMAC hash")
var ErrInvalidHmacHex = errors.New("client provided HMAC hash that could not be hex decoded")

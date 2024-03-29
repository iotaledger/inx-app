package httpserver

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/log"
	"github.com/iotaledger/hive.go/serializer/v2/serix"
	iotago "github.com/iotaledger/iota.go/v4"
	iotaapi "github.com/iotaledger/iota.go/v4/api"
	"github.com/iotaledger/iota.go/v4/hexutil"
)

const (
	ProtocolHTTP  = "http"
	ProtocolHTTPS = "https"
	ProtocolWS    = "ws"
	ProtocolWSS   = "wss"
)

var (
	// ErrInvalidParameter defines the invalid parameter error.
	ErrInvalidParameter = echo.NewHTTPError(http.StatusBadRequest, "invalid parameter")

	// ErrNotAcceptable defines the not acceptable error.
	ErrNotAcceptable = echo.NewHTTPError(http.StatusNotAcceptable)
)

// JSONResponse sends the JSON response with status code.
func JSONResponse(c echo.Context, statusCode int, result interface{}) error {
	return c.JSON(statusCode, result)
}

// HTTPErrorResponse defines the error struct for the HTTPErrorResponseEnvelope.
type HTTPErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// HTTPErrorResponseEnvelope defines the error response schema for node API responses.
type HTTPErrorResponseEnvelope struct {
	Error HTTPErrorResponse `json:"error"`
}

func errorHandler() func(error, echo.Context) {
	return func(err error, c echo.Context) {
		var statusCode int
		var message string

		var e *echo.HTTPError
		if ierrors.As(err, &e) {
			statusCode = e.Code
			message = fmt.Sprintf("%s, error: %s", e.Message, err)
		} else {
			statusCode = http.StatusInternalServerError
			message = fmt.Sprintf("internal server error. error: %s", err)
		}

		_ = c.JSON(statusCode, HTTPErrorResponseEnvelope{Error: HTTPErrorResponse{Code: strconv.Itoa(statusCode), Message: message}})
	}
}

// NewEcho returns a new Echo instance.
// It hides the banner, adds a default HTTPErrorHandler and the Recover middleware.
func NewEcho(logger log.Logger, onHTTPError func(err error, c echo.Context), debugRequestLoggerEnabled bool) *echo.Echo {
	e := echo.New()
	e.HideBanner = true

	apiErrorHandler := errorHandler()
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if onHTTPError != nil {
			onHTTPError(err, c)
		}
		apiErrorHandler(err, c)
	}

	e.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		LogErrorFunc: func(c echo.Context, err error, _ []byte) error {
			logger.LogErrorf("Internal Server Error: %s \nrequestURI: %s\n %s", err.Error(), c.Request().RequestURI, string(debug.Stack()))
			return err
		},
	}))

	if debugRequestLoggerEnabled {
		e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
			LogLatency:      true,
			LogRemoteIP:     true,
			LogMethod:       true,
			LogURI:          true,
			LogUserAgent:    true,
			LogStatus:       true,
			LogError:        true,
			LogResponseSize: true,
			LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
				errString := ""
				if v.Error != nil {
					errString = fmt.Sprintf("error: \"%s\", ", v.Error.Error())
				}

				logger.LogDebugf("%d %s \"%s\", %sagent: \"%s\", remoteIP: %s, responseSize: %s, took: %v", v.Status, v.Method, v.URI, errString, v.UserAgent, v.RemoteIP, humanize.Bytes(uint64(v.ResponseSize)), v.Latency.Truncate(time.Millisecond))

				return nil
			},
		}))
	}

	return e
}

func GetAcceptHeaderContentType(c echo.Context, supportedContentTypes ...string) (string, error) {
	ctype := c.Request().Header.Get(echo.HeaderAccept)
	for _, supportedContentType := range supportedContentTypes {
		if strings.HasPrefix(ctype, supportedContentType) {
			return supportedContentType, nil
		}
	}

	return "", ErrNotAcceptable
}

func GetRequestContentType(c echo.Context, supportedContentTypes ...string) (string, error) {
	ctype := c.Request().Header.Get(echo.HeaderContentType)
	for _, supportedContentType := range supportedContentTypes {
		if strings.HasPrefix(ctype, supportedContentType) {
			return supportedContentType, nil
		}
	}

	return "", echo.ErrUnsupportedMediaType
}

// ParseRequestByHeader parses the request based on the MIME type in the content header.
// Supported MIME types: IOTASerializerV2, JSON.
func ParseRequestByHeader[T any](c echo.Context, api iotago.API, binaryParserFunc func(bytes []byte) (T, int, error)) (T, error) {
	var obj T

	mimeType, err := GetRequestContentType(c, iotaapi.MIMEApplicationVendorIOTASerializerV2, echo.MIMEApplicationJSON)
	if err != nil {
		return obj, ierrors.Join(ErrInvalidParameter, err)
	}

	if c.Request().Body == nil {
		// bad request
		return obj, ierrors.Wrap(ErrInvalidParameter, "error: request body missing")
	}

	bytes, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return obj, ierrors.Wrapf(ErrInvalidParameter, "failed to read request body, error: %w", err)
	}

	switch mimeType {
	case echo.MIMEApplicationJSON:
		var err error

		reflectType := reflect.TypeOf(obj)
		if reflectType != nil && reflectType.Kind() == reflect.Pointer {
			// passed generic type is a pointer type
			// create a new instance of the type and decode into it
			//nolint:forcetypeassert // we know that obj is a pointer type
			obj = reflect.New(reflectType.Elem()).Interface().(T)
			err = api.JSONDecode(bytes, obj, serix.WithValidation())
		} else {
			err = api.JSONDecode(bytes, &obj, serix.WithValidation())
		}

		if err != nil {
			return obj, ierrors.Wrapf(ErrInvalidParameter, "failed to decode json data, error: %w", err)
		}

	case iotaapi.MIMEApplicationVendorIOTASerializerV2:
		obj, _, err = binaryParserFunc(bytes)
		if err != nil {
			return obj, ierrors.Wrapf(ErrInvalidParameter, "failed to parse binary data, error: %w", err)
		}

	default:
		return obj, echo.ErrUnsupportedMediaType
	}

	return obj, nil
}

// SendResponseByHeader sends the response based on the MIME type in the accept header.
// Supported MIME types: IOTASerializerV2, JSON.
// If the MIME type is not supported, or there is none, it defaults to JSON.
func SendResponseByHeader(c echo.Context, api iotago.API, obj any, httpStatusCode ...int) error {
	mimeType, err := GetAcceptHeaderContentType(c, iotaapi.MIMEApplicationVendorIOTASerializerV2, echo.MIMEApplicationJSON)
	if err != nil && !ierrors.Is(err, ErrNotAcceptable) {
		return err
	}

	statusCode := http.StatusOK
	if len(httpStatusCode) > 0 {
		statusCode = httpStatusCode[0]
	}

	switch mimeType {
	case iotaapi.MIMEApplicationVendorIOTASerializerV2:
		b, err := api.Encode(obj)
		if err != nil {
			return ierrors.Wrap(err, "failed to encode binary data")
		}

		return c.Blob(statusCode, iotaapi.MIMEApplicationVendorIOTASerializerV2, b)

	// default to echo.MIMEApplicationJSON
	default:
		j, err := api.JSONEncode(obj)
		if err != nil {
			return ierrors.Wrap(err, "failed to encode json data")
		}

		return c.JSONBlob(statusCode, j)
	}
}

// ParseBoolQueryParam parses the boolean query parameter.
// It returns an error if the query parameter is not set.
func ParseBoolQueryParam(c echo.Context, paramName string) (bool, error) {
	return strconv.ParseBool(c.QueryParam(paramName))
}

// ParseUint32QueryParam parses the uint32 query parameter.
// It returns an error if the query parameter is not set.
func ParseUint32QueryParam(c echo.Context, paramName string, maxValue ...uint32) (uint32, error) {
	intString := strings.ToLower(c.QueryParam(paramName))
	if intString == "" {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "parameter \"%s\" not specified", paramName)
	}

	value, err := strconv.ParseUint(intString, 10, 32)
	if err != nil {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, error: %w", intString, err)
	}

	if len(maxValue) > 0 {
		if uint32(value) > maxValue[0] {
			return 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, higher than the max number %d", intString, maxValue)
		}
	}

	return uint32(value), nil
}

// ParseSlotQueryParam parses the slot query parameter.
// It returns an error if the query parameter is not set.
func ParseSlotQueryParam(c echo.Context, paramName string) (iotago.SlotIndex, error) {
	slotParam := c.QueryParam(paramName)

	if slotParam == "" {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "parameter \"%s\" not specified", paramName)
	}

	value, err := strconv.ParseUint(slotParam, 10, 32)
	if err != nil {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, error: %w", slotParam, err)
	}

	return iotago.SlotIndex(value), nil
}

// ParseEpochQueryParam parses the epoch query parameter.
// It returns an error if the query parameter is not set.
func ParseEpochQueryParam(c echo.Context, paramName string) (iotago.EpochIndex, error) {
	epochParam := c.QueryParam(paramName)

	if epochParam == "" {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "parameter \"%s\" not specified", paramName)
	}

	value, err := strconv.ParseUint(epochParam, 10, 32)
	if err != nil {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, error: %w", epochParam, err)
	}

	return iotago.EpochIndex(value), nil
}

// ParseEpochCursorQueryParam parses the cursor query parameter.
// It returns an error if the query parameter is not set.
func ParseEpochCursorQueryParam(c echo.Context, paramName string) (iotago.EpochIndex, uint32, error) {
	cursor := c.QueryParam(paramName)
	if cursor == "" {
		return 0, 0, ierrors.Wrapf(ErrInvalidParameter, "parameter \"%s\" not specified", paramName)
	}
	cursorParts := strings.Split(cursor, ",")

	epochPart, err := strconv.ParseUint(cursorParts[0], 10, 32)
	if err != nil {
		return 0, 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, in parsing query parameter: %s error: %w", cursorParts[0], paramName, err)
	}
	startedAtEpoch := iotago.EpochIndex(epochPart)

	indexPart, err := strconv.ParseUint(cursorParts[1], 10, 32)
	if err != nil {
		return 0, 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, in parsing query parameter: %s error: %w", cursorParts[1], paramName, err)
	}
	index := uint32(indexPart)

	return startedAtEpoch, index, nil
}

// ParseSlotCursorQueryParam parses the cursor query parameter.
// It returns an error if the query parameter is not set.
func ParseSlotCursorQueryParam(c echo.Context, paramName string) (iotago.SlotIndex, uint32, error) {
	cursor := c.QueryParam(paramName)
	if cursor == "" {
		return 0, 0, ierrors.Wrapf(ErrInvalidParameter, "parameter \"%s\" not specified", paramName)
	}
	cursorParts := strings.Split(cursor, ",")

	slotPart, err := strconv.ParseUint(cursorParts[0], 10, 32)
	if err != nil {
		return 0, 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, in parsing query parameter: %s error: %w", cursorParts[0], paramName, err)
	}
	startedAtSlot := iotago.SlotIndex(slotPart)

	indexPart, err := strconv.ParseUint(cursorParts[1], 10, 32)
	if err != nil {
		return 0, 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, in parsing query parameter: %s error: %w", cursorParts[1], paramName, err)
	}
	index := uint32(indexPart)

	return startedAtSlot, index, nil
}

// ParsePageSizeQueryParam parses the page size query parameter.
// It returns the maxPageSize if the query parameter is not set or there was an error.
func ParsePageSizeQueryParam(c echo.Context, paramName string, maxPageSize uint32) uint32 {
	if len(c.QueryParam(paramName)) > 0 {
		pageSizeQueryParam, err := ParseUint32QueryParam(c, paramName, maxPageSize)
		if err != nil {
			return maxPageSize
		}

		// page size is already less or equal maxPageSize here
		return pageSizeQueryParam
	}

	return maxPageSize
}

// ParseHexQueryParam parses the hex query parameter.
// It returns an error if the query parameter is not set.
func ParseHexQueryParam(c echo.Context, paramName string, maxLen int) ([]byte, error) {
	param := c.QueryParam(paramName)

	paramBytes, err := hexutil.DecodeHex(param)
	if err != nil {
		return nil, ierrors.Wrapf(ErrInvalidParameter, "invalid param: %s, error: %w", paramName, err)
	}
	if len(paramBytes) > maxLen {
		return nil, ierrors.Wrapf(ErrInvalidParameter, "query parameter %s too long, max. %d bytes but is %d", paramName, maxLen, len(paramBytes))
	}

	return paramBytes, nil
}

// ParseUnixTimestampQueryParam parses the unix timestamp query parameter.
// It returns an error if the query parameter is not set.
func ParseUnixTimestampQueryParam(c echo.Context, paramName string) (time.Time, error) {
	timestamp, err := ParseUint32QueryParam(c, paramName)
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(int64(timestamp), 0), nil
}

// ParseBech32AddressQueryParam parses the bech32 address query parameter.
// It returns an error if the query parameter is not set.
func ParseBech32AddressQueryParam(c echo.Context, prefix iotago.NetworkPrefix, paramName string) (iotago.Address, error) {
	addressParam := strings.ToLower(c.QueryParam(paramName))

	hrp, bech32Address, err := iotago.ParseBech32(addressParam)
	if err != nil {
		return nil, ierrors.Wrapf(ErrInvalidParameter, "invalid address: %s, error: %w", addressParam, err)
	}

	if hrp != prefix {
		return nil, ierrors.Wrapf(ErrInvalidParameter, "invalid bech32 address, expected prefix: %s", prefix)
	}

	return bech32Address, nil
}

// ParseCommitmentIDQueryParam parses the commitment ID query parameter.
// It returns EmptyCommitmentID if the query parameter is not set.
func ParseCommitmentIDQueryParam(c echo.Context, paramName string) (iotago.CommitmentID, error) {
	commitmentIDHex := strings.ToLower(c.QueryParam(paramName))

	if len(commitmentIDHex) == 0 {
		return iotago.EmptyCommitmentID, nil
	}

	commitmentID, err := iotago.CommitmentIDFromHexString(commitmentIDHex)
	if err != nil {
		return iotago.EmptyCommitmentID, ierrors.Wrapf(ErrInvalidParameter, "invalid commitment ID: %s, error: %w", commitmentIDHex, err)
	}

	return commitmentID, nil
}

// ParseWorkScoreQueryParam parses the work score query parameter.
// It returns 0 if the query parameter is not set.
func ParseWorkScoreQueryParam(c echo.Context, paramName string) (iotago.WorkScore, error) {
	if len(c.QueryParam(paramName)) == 0 {
		return 0, nil
	}

	workScore, err := ParseUint32QueryParam(c, paramName)
	if err != nil {
		return 0, err
	}

	return iotago.WorkScore(workScore), nil
}

// ParseCommitmentIDParam parses the commitment ID parameter.
func ParseCommitmentIDParam(c echo.Context, paramName string) (iotago.CommitmentID, error) {
	commitmentIDHex := strings.ToLower(c.Param(paramName))

	commitmentID, err := iotago.CommitmentIDFromHexString(commitmentIDHex)
	if err != nil {
		return iotago.EmptyCommitmentID, ierrors.Wrapf(ErrInvalidParameter, "invalid commitment ID: %s, error: %w", commitmentIDHex, err)
	}

	return commitmentID, nil
}

// ParseBlockIDParam parses the block ID parameter.
func ParseBlockIDParam(c echo.Context, paramName string) (iotago.BlockID, error) {
	blockIDHex := strings.ToLower(c.Param(paramName))

	blockIDs, err := iotago.BlockIDsFromHexString([]string{blockIDHex})
	if err != nil {
		return iotago.EmptyBlockID, ierrors.Wrapf(ErrInvalidParameter, "invalid block ID: %s, error: %w", blockIDHex, err)
	}

	return blockIDs[0], nil
}

// ParseTransactionIDParam parses the transaction ID parameter.
func ParseTransactionIDParam(c echo.Context, paramName string) (iotago.TransactionID, error) {
	transactionID := iotago.TransactionID{}
	transactionIDHex := strings.ToLower(c.Param(paramName))

	transactionIDBytes, err := hexutil.DecodeHex(transactionIDHex)
	if err != nil {
		return transactionID, ierrors.Wrapf(ErrInvalidParameter, "invalid transaction ID: %s, error: %w", transactionIDHex, err)
	}

	if len(transactionIDBytes) != iotago.TransactionIDLength {
		return transactionID, ierrors.Wrapf(ErrInvalidParameter, "invalid transaction ID: %s, invalid length: %d", transactionIDHex, len(transactionIDBytes))
	}

	copy(transactionID[:], transactionIDBytes)

	return transactionID, nil
}

// ParseOutputIDParam parses the output ID parameter.
func ParseOutputIDParam(c echo.Context, paramName string) (iotago.OutputID, error) {
	outputIDParam := strings.ToLower(c.Param(paramName))

	outputID, err := iotago.OutputIDFromHexString(outputIDParam)
	if err != nil {
		return iotago.OutputID{}, ierrors.Wrapf(ErrInvalidParameter, "invalid output ID: %s, error: %w", outputIDParam, err)
	}

	return outputID, nil
}

// ParseFoundryIDParam parses the foundry ID parameter.
func ParseFoundryIDParam(c echo.Context, paramName string) (iotago.FoundryID, error) {
	foundryID := iotago.FoundryID{}
	foundryIDHex := strings.ToLower(c.Param(paramName))

	foundryIDBytes, err := hexutil.DecodeHex(foundryIDHex)
	if err != nil {
		return foundryID, ierrors.Wrapf(ErrInvalidParameter, "invalid foundry ID: %s, error: %w", foundryIDHex, err)
	}

	if len(foundryIDBytes) != iotago.FoundryIDLength {
		return foundryID, ierrors.Wrapf(ErrInvalidParameter, "invalid foundryID: %s, invalid length: %d", foundryIDHex, len(foundryIDBytes))
	}

	copy(foundryID[:], foundryIDBytes)

	return foundryID, nil
}

// ParseDelegationIDParam parses the delegation ID parameter.
func ParseDelegationIDParam(c echo.Context, paramName string) (iotago.DelegationID, error) {
	delegationID := iotago.DelegationID{}
	delegationIDHex := strings.ToLower(c.Param(paramName))

	delegationIDBytes, err := hexutil.DecodeHex(delegationIDHex)
	if err != nil {
		return delegationID, ierrors.Wrapf(ErrInvalidParameter, "invalid delegationID: %s, error: %w", delegationIDHex, err)
	}

	if len(delegationIDBytes) != iotago.DelegationIDLength {
		return delegationID, ierrors.Wrapf(ErrInvalidParameter, "invalid delegationID: %s, invalid length: %d", delegationIDHex, len(delegationIDBytes))
	}

	copy(delegationID[:], delegationIDBytes)

	return delegationID, nil
}

// ParseBech32AddressParam parses the bech32 address parameter.
func ParseBech32AddressParam(c echo.Context, prefix iotago.NetworkPrefix, paramName string) (iotago.Address, error) {
	addressParam := strings.ToLower(c.Param(paramName))

	hrp, bech32Address, err := iotago.ParseBech32(addressParam)
	if err != nil {
		return nil, ierrors.Wrapf(ErrInvalidParameter, "invalid address: %s, error: %w", addressParam, err)
	}

	if hrp != prefix {
		return nil, ierrors.Wrapf(ErrInvalidParameter, "invalid bech32 address, expected prefix: %s", prefix)
	}

	return bech32Address, nil
}

// ParseUint64Param parses the uint64 parameter.
func ParseUint64Param(c echo.Context, paramName string, maxValue ...uint64) (uint64, error) {
	intString := strings.ToLower(c.Param(paramName))
	if intString == "" {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "parameter \"%s\" not specified", paramName)
	}

	value, err := strconv.ParseUint(intString, 10, 64)
	if err != nil {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, error: %w", intString, err)
	}

	if len(maxValue) > 0 {
		if value > maxValue[0] {
			return 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, higher than the max number %d", intString, maxValue)
		}
	}

	return value, nil
}

// ParseSlotParam parses the slot parameter.
func ParseSlotParam(c echo.Context, paramName string) (iotago.SlotIndex, error) {
	slotParam := strings.ToLower(c.Param(paramName))

	if slotParam == "" {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "parameter \"%s\" not specified", paramName)
	}

	value, err := strconv.ParseUint(slotParam, 10, 32)
	if err != nil {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, error: %w", slotParam, err)
	}

	return iotago.SlotIndex(value), nil
}

// GetURL joins the protocol, host, port and path to a URL.
func GetURL(protocol string, host string, port uint16, path ...string) string {
	return fmt.Sprintf("%s://%s%s", protocol, net.JoinHostPort(host, strconv.Itoa(int(port))), strings.Join(path, "/"))
}

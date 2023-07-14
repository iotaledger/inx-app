package httpserver

import (
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/logger"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/hexutil"
)

const (
	MIMEApplicationVendorIOTASerializerV1 = "application/vnd.iota.serializer-v1"
	ProtocolHTTP                          = "http"
	ProtocolHTTPS                         = "https"
	ProtocolWS                            = "ws"
	ProtocolWSS                           = "wss"
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
func NewEcho(logger *logger.Logger, onHTTPError func(err error, c echo.Context), debugRequestLoggerEnabled bool) *echo.Echo {
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
		LogErrorFunc: func(c echo.Context, err error, stack []byte) error {
			logger.Errorf("Internal Server Error: %s \nrequestURI: %s\n %s", err.Error(), c.Request().RequestURI, string(debug.Stack()))
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
			LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
				errString := ""
				if v.Error != nil {
					errString = fmt.Sprintf("error: \"%s\", ", v.Error.Error())
				}

				logger.Debugf("%d %s \"%s\", %sagent: \"%s\", remoteIP: %s, responseSize: %s, took: %v", v.Status, v.Method, v.URI, errString, v.UserAgent, v.RemoteIP, humanize.Bytes(uint64(v.ResponseSize)), v.Latency.Truncate(time.Millisecond))

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

func ParseBoolQueryParam(c echo.Context, paramName string) (bool, error) {
	return strconv.ParseBool(c.QueryParam(paramName))
}

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

func ParseSlotQueryParam(c echo.Context, paramName string) (iotago.SlotIndex, error) {
	param := c.QueryParam(paramName)

	if param == "" {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "parameter \"%s\" not specified", paramName)
	}

	value, err := strconv.ParseUint(param, 10, 64)
	if err != nil {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, error: %s", param, err)
	}

	return iotago.SlotIndex(value), nil
}

func ParseEpochQueryParam(c echo.Context, paramName string) (iotago.EpochIndex, error) {
	param := c.QueryParam(paramName)

	if param == "" {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "parameter \"%s\" not specified", paramName)
	}

	value, err := strconv.ParseUint(param, 10, 64)
	if err != nil {
		return 0, ierrors.Wrapf(ErrInvalidParameter, "invalid value: %s, error: %s", param, err)
	}

	return iotago.EpochIndex(value), nil
}

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

func ParseUnixTimestampQueryParam(c echo.Context, paramName string) (time.Time, error) {
	timestamp, err := ParseUint32QueryParam(c, paramName)
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(int64(timestamp), 0), nil
}

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

func ParseCommitmentIDParam(c echo.Context, paramName string) (iotago.CommitmentID, error) {
	commitmentIDHex := strings.ToLower(c.Param(paramName))

	commitmentIDs, err := iotago.BlockIDsFromHexString([]string{commitmentIDHex})
	if err != nil {
		return iotago.EmptyBlockID(), ierrors.Wrapf(ErrInvalidParameter, "invalid commitment ID: %s, error: %w", commitmentIDHex, err)
	}

	return commitmentIDs[0], nil
}

func ParseBlockIDParam(c echo.Context, paramName string) (iotago.BlockID, error) {
	blockIDHex := strings.ToLower(c.Param(paramName))

	blockIDs, err := iotago.BlockIDsFromHexString([]string{blockIDHex})
	if err != nil {
		return iotago.EmptyBlockID(), ierrors.Wrapf(ErrInvalidParameter, "invalid block ID: %s, error: %w", blockIDHex, err)
	}

	return blockIDs[0], nil
}

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

func ParseOutputIDParam(c echo.Context, paramName string) (iotago.OutputID, error) {
	outputIDParam := strings.ToLower(c.Param(paramName))

	outputID, err := iotago.OutputIDFromHex(outputIDParam)
	if err != nil {
		return iotago.OutputID{}, ierrors.Wrapf(ErrInvalidParameter, "invalid output ID: %s, error: %w", outputIDParam, err)
	}

	return outputID, nil
}

func ParseAccountIDParam(c echo.Context, paramName string) (iotago.AccountID, error) {
	accountIDParam := strings.ToLower(c.Param(paramName))

	accountIDBytes, err := hexutil.DecodeHex(accountIDParam)
	if err != nil {
		return iotago.AccountID{}, ierrors.Wrapf(ErrInvalidParameter, "invalid account ID: %s, error: %w", accountIDParam, err)
	}

	if len(accountIDBytes) != iotago.AccountIDLength {
		return iotago.AccountID{}, ierrors.Wrapf(ErrInvalidParameter, "invalid account ID: %s, error: %w", accountIDParam, err)
	}

	var accountID iotago.AccountID
	copy(accountID[:], accountIDBytes)

	return iotago.AccountID{}, nil
}

func ParseNFTIDParam(c echo.Context, paramName string) (*iotago.NFTID, error) {
	nftIDParam := strings.ToLower(c.Param(paramName))

	nftIDBytes, err := hexutil.DecodeHex(nftIDParam)
	if err != nil {
		return nil, ierrors.Wrapf(ErrInvalidParameter, "invalid NFT ID: %s, error: %w", nftIDParam, err)
	}

	if len(nftIDBytes) != iotago.NFTIDLength {
		return nil, ierrors.Wrapf(ErrInvalidParameter, "invalid NFT ID: %s, error: %w", nftIDParam, err)
	}

	var nftID iotago.NFTID
	copy(nftID[:], nftIDBytes)

	return &nftID, nil
}

func ParseFoundryIDParam(c echo.Context, paramName string) (*iotago.FoundryID, error) {
	foundryIDParam := strings.ToLower(c.Param(paramName))

	foundryIDBytes, err := hexutil.DecodeHex(foundryIDParam)
	if err != nil {
		return nil, ierrors.Wrapf(ErrInvalidParameter, "invalid foundry ID: %s, error: %w", foundryIDParam, err)
	}

	if len(foundryIDBytes) != iotago.FoundryIDLength {
		return nil, ierrors.Wrapf(ErrInvalidParameter, "invalid foundry ID: %s, error: %w", foundryIDParam, err)
	}

	var foundryID iotago.FoundryID
	copy(foundryID[:], foundryIDBytes)

	return &foundryID, nil
}

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

func GetURL(protocol string, host string, port uint16, path ...string) string {
	return fmt.Sprintf("%s://%s%s", protocol, net.JoinHostPort(host, strconv.Itoa(int(port))), strings.Join(path, "/"))
}

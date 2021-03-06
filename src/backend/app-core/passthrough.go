package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/labstack/echo"
	"github.com/labstack/echo/engine"
	"github.com/labstack/echo/engine/standard"

	"github.com/SUSE/stratos-ui/repository/interfaces"
)

// API Host Prefix to replace if the custom header is supplied
const apiPrefix = "api."

func getEchoURL(c echo.Context) url.URL {
	log.Debug("getEchoURL")
	u := c.Request().URL().(*standard.URL).URL

	// dereference so we get a copy
	return *u
}

func getEchoHeaders(c echo.Context) http.Header {
	log.Debug("getEchoHeaders")
	h := make(http.Header)
	originalHeader := c.Request().Header().(*standard.Header).Header
	for k, v := range originalHeader {
		if k == "Cookie" {
			continue
		}
		vCopy := make([]string, len(v))
		copy(vCopy, v)
		h[k] = vCopy
	}

	return h
}

func makeRequestURI(c echo.Context) *url.URL {
	log.Debug("makeRequestURI")
	uri := getEchoURL(c)
	prefix := strings.TrimSuffix(c.Path(), "*")
	uri.Path = strings.TrimPrefix(uri.Path, prefix)

	return &uri
}

func getPortalUserGUID(c echo.Context) (string, error) {
	log.Debug("getPortalUserGUID")
	portalUserGUIDIntf := c.Get("user_id")
	if portalUserGUIDIntf == nil {
		return "", errors.New("Corrupted session")
	}
	return portalUserGUIDIntf.(string), nil
}

func getRequestParts(c echo.Context) (engine.Request, []byte, error) {
	log.Debug("getRequestParts")
	var body []byte
	var err error
	req := c.Request()
	if bodyReader := req.Body(); bodyReader != nil {
		if body, err = ioutil.ReadAll(bodyReader); err != nil {
			return nil, nil, errors.New("Failed to read request body")
		}
	}
	return req, body, nil
}

func buildJSONResponse(cnsiList []string, responses map[string]*interfaces.CNSIRequest) map[string]*json.RawMessage {
	log.Debug("buildJSONResponse")
	jsonResponse := make(map[string]*json.RawMessage)
	for _, guid := range cnsiList {
		var response []byte
		cnsiResponse, ok := responses[guid]
		switch {
		case !ok:
			response = []byte(`{"error": {"statusCode": 500, "status": "Request timed out"}}`)
		case cnsiResponse.Error != nil:
			response = []byte(fmt.Sprintf(`{"error": {"statusCode": 500, "status": "%q"}}`, cnsiResponse.Error.Error()))
		case cnsiResponse.Response != nil:
			response = cnsiResponse.Response
		}
		// Check the HTTP Status code to make sure that it is actually a valid response
		if cnsiResponse.StatusCode >= 400 {
			errorJson, err := json.Marshal(cnsiResponse)
			if err != nil {
				errorJson = []byte(fmt.Sprintf(`{"statusCode": 500, "status": "Failed to proxy request"}"`))
			}
			response = []byte(fmt.Sprintf(`{"error": %s, "errorResponse": %s}`, errorJson, string(cnsiResponse.Response)))
		}
		if len(response) > 0 {
			jsonResponse[guid] = (*json.RawMessage)(&response)
		} else {
			jsonResponse[guid] = nil
		}
	}

	return jsonResponse
}

func (p *portalProxy) buildCNSIRequest(cnsiGUID string, userGUID string, method string, uri *url.URL, body []byte, header http.Header) (interfaces.CNSIRequest, error) {
	log.Debug("buildCNSIRequest")
	cnsiRequest := interfaces.CNSIRequest{
		GUID:     cnsiGUID,
		UserGUID: userGUID,

		Method: method,
		Body:   body,
		Header: header,
	}

	cnsiRec, err := p.GetCNSIRecord(cnsiGUID)
	if err != nil {
		return cnsiRequest, err
	}

	cnsiRequest.URL = new(url.URL)
	*cnsiRequest.URL = *cnsiRec.APIEndpoint
	cnsiRequest.URL.Path = uri.Path
	cnsiRequest.URL.RawQuery = uri.RawQuery

	return cnsiRequest, nil
}

func (p *portalProxy) validateCNSIList(cnsiList []string) error {
	log.Debug("validateCNSIList")
	for _, cnsiGUID := range cnsiList {
		if _, err := p.GetCNSIRecord(cnsiGUID); err != nil {
			return err
		}
	}

	return nil
}

func fwdCNSIStandardHeaders(cnsiRequest *interfaces.CNSIRequest, req *http.Request) {
	log.Debug("fwdCNSIStandardHeaders")
	for k, v := range cnsiRequest.Header {
		switch {
		// Skip these
		//  - "Referer" causes CF to fail with a 403
		//  - "Connection", "X-Cap-*" and "Cookie" are consumed by us
		case k == "Connection", k == "Cookie", k == "Referer", strings.HasPrefix(strings.ToLower(k), "x-cap-"):

		// Forwarding everything else
		default:
			req.Header[k] = v
		}
	}
}

func (p *portalProxy) proxy(c echo.Context) error {
	log.Debug("proxy")
	responses, err := p.ProxyRequest(c, makeRequestURI(c))
	if err != nil {
		return err
	}

	return p.SendProxiedResponse(c, responses)
}

func (p *portalProxy) ProxyRequest(c echo.Context, uri *url.URL) (map[string]*interfaces.CNSIRequest, error) {
	log.Debug("proxy")
	cnsiList := strings.Split(c.Request().Header().Get("x-cap-cnsi-list"), ",")
	shouldPassthrough := "true" == c.Request().Header().Get("x-cap-passthrough")

	if err := p.validateCNSIList(cnsiList); err != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	header := getEchoHeaders(c)
	header.Del("Cookie")

	portalUserGUID, err := getPortalUserGUID(c)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	req, body, err := getRequestParts(c)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if shouldPassthrough {
		if len(cnsiList) > 1 {
			err := errors.New("Requested passthrough to multiple CNSIs. Only single CNSI passthroughs are supported.")
			return nil, echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	// send the request to each CNSI
	done := make(chan *interfaces.CNSIRequest)
	for _, cnsi := range cnsiList {
		cnsiRequest, buildErr := p.buildCNSIRequest(cnsi, portalUserGUID, req.Method(), uri, body, header)
		if buildErr != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, buildErr.Error())
		}
		// Allow the host part of the API URL to be overridden
		apiHost := c.Request().Header().Get("x-cap-api-host")
		// Don't allow any '.' chars in the api name
		if apiHost != "" && !strings.ContainsAny(apiHost, ".") {
			// Add trailing . for when we replace
			apiHost = apiHost + "."
			// Override the API URL if needed
			if strings.HasPrefix(cnsiRequest.URL.Host, apiPrefix) {
				// Replace 'api.' prefix with supplied prefix
				cnsiRequest.URL.Host = strings.Replace(cnsiRequest.URL.Host, apiPrefix, apiHost, 1)
			} else {
				// Add supplied prefix to the domain
				cnsiRequest.URL.Host = apiHost + cnsiRequest.URL.Host
			}
		}
		go p.doRequest(&cnsiRequest, done)
	}

	responses := make(map[string]*interfaces.CNSIRequest)
	for range cnsiList {
		res := <-done
		responses[res.GUID] = res
	}

	return responses, nil
}

func (p *portalProxy) DoProxyRequest(requests []interfaces.ProxyRequestInfo) (map[string]*interfaces.CNSIRequest, error) {
	log.Debug("DoProxyRequest")

	// send the request to each endpoint
	done := make(chan *interfaces.CNSIRequest)
	for _, requestInfo := range requests {
		cnsiRequest, buildErr := p.buildCNSIRequest(requestInfo.EndpointGUID, requestInfo.UserGUID, requestInfo.Method, requestInfo.URI, requestInfo.Body, requestInfo.Headers)
		cnsiRequest.ResponseGUID = requestInfo.ResultGUID
		if buildErr != nil {
			return nil, echo.NewHTTPError(http.StatusBadRequest, buildErr.Error())
		}
		go p.doRequest(&cnsiRequest, done)
	}

	responses := make(map[string]*interfaces.CNSIRequest)
	for range requests {
		res := <-done
		responses[res.ResponseGUID] = res
	}

	return responses, nil
}

func (p *portalProxy) SendProxiedResponse(c echo.Context, responses map[string]*interfaces.CNSIRequest) error {
	shouldPassthrough := "true" == c.Request().Header().Get("x-cap-passthrough")

	var cnsiList []string
	for k := range responses {
		cnsiList = append(cnsiList, k)
	}

	if shouldPassthrough {
		cnsiGUID := cnsiList[0]
		res, ok := responses[cnsiGUID]
		if !ok {
			return echo.NewHTTPError(http.StatusRequestTimeout, "Request timed out")
		}

		// in passthrough mode, set the status code to that of the single response
		c.Response().WriteHeader(res.StatusCode)

		// we don't care if this fails
		_, _ = c.Response().Write(res.Response)

		return nil
	}

	jsonResponse := buildJSONResponse(cnsiList, responses)
	e := json.NewEncoder(c.Response())
	err := e.Encode(jsonResponse)
	if err != nil {
		log.Errorf("Failed to encode JSON: %v\n%#v\n", err, jsonResponse)
	}
	return err
}

func (p *portalProxy) doRequest(cnsiRequest *interfaces.CNSIRequest, done chan<- *interfaces.CNSIRequest) {
	log.Debug("doRequest")
	var body io.Reader
	var res *http.Response
	var req *http.Request
	var err error

	if len(cnsiRequest.Body) > 0 {
		body = bytes.NewReader(cnsiRequest.Body)
	}
	req, err = http.NewRequest(cnsiRequest.Method, cnsiRequest.URL.String(), body)
	if err != nil {
		cnsiRequest.Error = err
		if done != nil {
			done <- cnsiRequest
		}
		return
	}

	// get a cnsi token record and a cnsi record
	tokenRec, _, err := p.getCNSIRequestRecords(cnsiRequest)
	if err != nil {
		cnsiRequest.Error = err
		if done != nil {
			done <- cnsiRequest
		}
		return
	}

	// Copy original headers through, except custom portal-proxy Headers
	fwdCNSIStandardHeaders(cnsiRequest, req)

	// Mkae the request using the appropriate auth helper
	switch tokenRec.AuthType {
	case interfaces.AuthTypeHttpBasic:
		res, err = p.doHttpBasicFlowRequest(cnsiRequest, req)
	case interfaces.AuthTypeOIDC:
		res, err = p.doOidcFlowRequest(cnsiRequest, req)
	default:
		res, err = p.doOauthFlowRequest(cnsiRequest, req)
	}

	if err != nil {
		cnsiRequest.StatusCode = 500
		cnsiRequest.Response = []byte(err.Error())
		cnsiRequest.Error = err
	} else if res.Body != nil {
		cnsiRequest.StatusCode = res.StatusCode
		cnsiRequest.Response, cnsiRequest.Error = ioutil.ReadAll(res.Body)
		defer res.Body.Close()
	}

	if done != nil {
		done <- cnsiRequest
	}
}

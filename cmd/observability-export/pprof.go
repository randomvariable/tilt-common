// Copyright 2026 Naadir Jeewa
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// Parca HTTP API paths.
const (
	parcaGatewayProfileTypesPath = "/profiles/types"
	parcaGatewayQueryPath        = "/profiles/query"
	parcaGRPCWebProfileTypesPath = "/api/parca.query.v1alpha1.QueryService/ProfileTypes"
	parcaGRPCWebQueryPath        = "/api/parca.query.v1alpha1.QueryService/Query"

	parcaProfileTypesPath = "/api/v1/profile_types"
	parcaQueryPath        = "/api/v1/query"

	legacyProfileTypesPath = "/parca.query.v1alpha1.QueryService/ProfileTypes"
	legacyQueryPath        = "/parca.query.v1alpha1.QueryService/Query"
)

type parcaAPI struct {
	profileTypesPath string
	queryPath        string
	profileTypesPOST bool
	connectProtocol  bool
	grpcWeb          bool
}

var parcaAPICandidates = []parcaAPI{
	{profileTypesPath: parcaGRPCWebProfileTypesPath, queryPath: parcaGRPCWebQueryPath, profileTypesPOST: true, grpcWeb: true},
	{profileTypesPath: parcaGatewayProfileTypesPath, queryPath: parcaGatewayQueryPath},
	{profileTypesPath: parcaProfileTypesPath, queryPath: parcaQueryPath},
	{profileTypesPath: legacyProfileTypesPath, queryPath: legacyQueryPath, profileTypesPOST: true, connectProtocol: true},
}

// parcaProfileType is a single profile type entry from Parca response payloads.
type parcaProfileType struct {
	Name string `json:"name"`
}

type parcaQueryReport struct {
	Pprof string `json:"pprof"`
}

// parcaQueryRequest is the JSON request body for a profile query.
// Mode 1 = QUERY_REQUEST_MODE_MERGE (merge all samples in the time range).
// ReportType 1 = QUERY_REQUEST_REPORT_TYPE_PPROF.
type parcaQueryRequest struct {
	Mode       int    `json:"mode"`
	Query      string `json:"query"`
	ReportType int    `json:"reportType"`
	Start      string `json:"start"`
	End        string `json:"end"`
}

// parcaQueryResponse models common JSON response variants from Parca's query API.
type parcaQueryResponse struct {
	Pprof  string           `json:"pprof"`
	Report parcaQueryReport `json:"report"`
	Data   parcaQueryReport `json:"data"`
}

// ExportProfiles downloads pprof profiles from Parca via HTTP API
// and saves them to a profiles subdirectory of outputDir. If Parca is
// unavailable or has no profiles, a warning is logged and nil is returned.
func ExportProfiles(ctx context.Context, parcaURL, outputDir string, client *http.Client) error {
	profilesDir := filepath.Join(outputDir, "profiles")
	if err := os.MkdirAll(profilesDir, dirPermissions); err != nil {
		return fmt.Errorf("creating profiles directory: %w", err)
	}

	api, profileTypes, err := discoverParcaAPI(ctx, client, parcaURL)
	if err != nil {
		slog.Warn("parca unavailable, skipping profile export", "url", parcaURL, "error", err)

		return nil
	}

	slog.Info("detected Parca API", "profile_types_path", api.profileTypesPath, "query_path", api.queryPath)

	if len(profileTypes) == 0 {
		slog.Info("no profile types found in Parca")

		return nil
	}

	slog.Info("discovered profile types", "count", len(profileTypes), "types", profileTypes)

	var downloaded int

	for _, profileType := range profileTypes {
		if err := downloadProfile(ctx, client, parcaURL, api, profileType, profilesDir); err != nil {
			slog.Warn("failed to download profile", "type", profileType, "error", err)

			continue
		}

		downloaded++
	}

	slog.Info("profiles downloaded", "count", downloaded)

	return nil
}

//nolint:err113 // includes endpoint path details to aid API/protocol troubleshooting.
func discoverParcaAPI(ctx context.Context, client *http.Client, parcaURL string) (parcaAPI, []string, error) {
	var errs []string

	for _, candidate := range parcaAPICandidates {
		types, err := fetchProfileTypesWithAPI(ctx, client, parcaURL, candidate)
		if err == nil {
			return candidate, types, nil
		}

		errs = append(errs, fmt.Sprintf("%s: %v", candidate.profileTypesPath, err))
	}

	return parcaAPI{}, nil, fmt.Errorf("no Parca API endpoint detected (%s)", strings.Join(errs, "; "))
}

// fetchProfileTypes queries Parca for the list of available profile type names.
func fetchProfileTypes(ctx context.Context, parcaURL string) ([]string, error) {
	_, profileTypes, err := discoverParcaAPI(ctx, nil, parcaURL)

	return profileTypes, err
}

//nolint:cyclop,err113 // endpoint probing intentionally branches across protocol variants.
func fetchProfileTypesWithAPI(ctx context.Context, client *http.Client, parcaURL string, api parcaAPI) ([]string, error) {
	if api.grpcWeb {
		payload, err := grpcWebUnary(ctx, clientOrDefault(client), parcaURL, api.profileTypesPath, nil)
		if err != nil {
			return nil, fmt.Errorf("fetching profile types over grpc-web: %w", err)
		}

		names, err := parseProfileTypeNamesProto(payload)
		if err != nil {
			return nil, fmt.Errorf("decoding grpc-web profile types response: %w", err)
		}

		return names, nil
	}

	parsedURL, err := url.Parse(parcaURL)
	if err != nil {
		return nil, fmt.Errorf("parsing Parca URL: %w", err)
	}

	parsedURL.Path = api.profileTypesPath

	method := http.MethodGet

	var reqBody io.Reader = http.NoBody

	if api.profileTypesPOST {
		method = http.MethodPost
		reqBody = bytes.NewReader([]byte("{}"))
	}

	req, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating profile types request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if api.connectProtocol {
		req.Header.Set("Connect-Protocol-Version", "1")
	}

	resp, err := clientOrDefault(client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching profile types: %w", err)
	}

	defer closeResource("profile types response body", resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d from %s", ErrUnexpectedHTTPStatus, resp.StatusCode, api.profileTypesPath)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading profile types response: %w", err)
	}

	if looksLikeHTML(resp.Header.Get("Content-Type"), body) {
		return nil, fmt.Errorf("profile types endpoint returned HTML (likely SPA) from %s", api.profileTypesPath)
	}

	names, err := parseProfileTypeNames(body)
	if err != nil {
		return nil, fmt.Errorf("decoding profile types response: %w", err)
	}

	return names, nil
}

// downloadProfile queries Parca for all samples of a given profile type across
// the full available time range and saves the pprof binary to profilesDir.
//
//nolint:cyclop,err113,funlen // profile download path handles multiple API variants and response shapes.
func downloadProfile(ctx context.Context, client *http.Client, parcaURL string, api parcaAPI, profileType, profilesDir string) error {
	now := time.Now().UTC()

	if api.grpcWeb {
		pprofData, err := downloadProfileGRPCWeb(ctx, clientOrDefault(client), parcaURL, api, profileType, now)
		if err != nil {
			return err
		}

		safeName := sanitizeProfileName(profileType)
		timestamp := now.Format(TimestampFormat)
		filename := filepath.Clean(filepath.Join(profilesDir, fmt.Sprintf("%s-%s.pb.gz", safeName, timestamp)))

		profileFile, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("creating profile file %q: %w", filename, err)
		}

		defer closeResource("profile file", profileFile)

		written, err := io.Copy(profileFile, bytes.NewReader(pprofData))
		if err != nil {
			return fmt.Errorf("writing profile data: %w", err)
		}

		slog.Info("profile saved", "type", profileType, "file", filename, "bytes", written)

		return nil
	}

	parsedURL, err := url.Parse(parcaURL)
	if err != nil {
		return fmt.Errorf("parsing Parca URL: %w", err)
	}

	parsedURL.Path = api.queryPath

	queryReq := parcaQueryRequest{
		Mode:       1, // QUERY_REQUEST_MODE_MERGE
		Query:      profileType + "{}",
		ReportType: 1, // QUERY_REQUEST_REPORT_TYPE_PPROF
		Start:      "1970-01-01T00:00:00Z",
		End:        now.Format(time.RFC3339),
	}

	reqBody, err := json.Marshal(queryReq)
	if err != nil {
		return fmt.Errorf("encoding query request for %q: %w", profileType, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("creating query request for %q: %w", profileType, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if api.connectProtocol {
		req.Header.Set("Connect-Protocol-Version", "1")
	}

	resp, err := clientOrDefault(client).Do(req)
	if err != nil {
		return fmt.Errorf("querying profile %q: %w", profileType, err)
	}

	defer closeResource("profile query response body", resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %d querying profile %q", ErrUnexpectedHTTPStatus, resp.StatusCode, profileType)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading query response for %q: %w", profileType, err)
	}

	if looksLikeHTML(resp.Header.Get("Content-Type"), body) {
		return fmt.Errorf("query endpoint returned HTML (likely SPA) for profile %q", profileType)
	}

	var queryResp parcaQueryResponse
	if err := json.Unmarshal(body, &queryResp); err != nil {
		return fmt.Errorf("decoding query response for %q: %w", profileType, err)
	}

	pprofBase64 := queryResp.Pprof
	if pprofBase64 == "" {
		pprofBase64 = queryResp.Report.Pprof
	}

	if pprofBase64 == "" {
		pprofBase64 = queryResp.Data.Pprof
	}

	if pprofBase64 == "" {
		return fmt.Errorf("no pprof data in response for profile type %q", profileType)
	}

	pprofData, err := base64.StdEncoding.DecodeString(pprofBase64)
	if err != nil {
		return fmt.Errorf("decoding pprof base64 for %q: %w", profileType, err)
	}

	safeName := sanitizeProfileName(profileType)
	timestamp := now.Format(TimestampFormat)
	filename := filepath.Clean(filepath.Join(profilesDir, fmt.Sprintf("%s-%s.pb.gz", safeName, timestamp)))

	profileFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("creating profile file %q: %w", filename, err)
	}

	defer closeResource("profile file", profileFile)

	written, err := io.Copy(profileFile, bytes.NewReader(pprofData))
	if err != nil {
		return fmt.Errorf("writing profile data: %w", err)
	}

	slog.Info("profile saved", "type", profileType, "file", filename, "bytes", written)

	return nil
}

//nolint:err113 // returns contextual errors for remote API/debugging clarity.
func downloadProfileGRPCWeb(ctx context.Context, client *http.Client, parcaURL string, api parcaAPI, profileType string, now time.Time) ([]byte, error) {
	requestPayload := buildQueryRequestProto(profileType, now)

	responsePayload, err := grpcWebUnary(ctx, client, parcaURL, api.queryPath, requestPayload)
	if err != nil {
		return nil, fmt.Errorf("querying profile %q over grpc-web: %w", profileType, err)
	}

	pprofData, err := parseQueryResponsePprofProto(responsePayload)
	if err != nil {
		return nil, fmt.Errorf("decoding grpc-web query response for %q: %w", profileType, err)
	}

	if len(pprofData) == 0 {
		return nil, fmt.Errorf("no pprof data in grpc-web response for profile type %q", profileType)
	}

	return pprofData, nil
}

// sanitizeProfileName converts a profile type string (e.g.,
// "process_cpu:cpu:nanoseconds:cpu:nanoseconds") into a safe filename
// component by replacing colons and slashes with underscores.
func sanitizeProfileName(profileType string) string {
	replacer := strings.NewReplacer(
		":", "_",
		"/", "_",
		" ", "_",
	)

	return replacer.Replace(profileType)
}

func parseProfileTypeNames(body []byte) ([]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decoding profile types payload: %w", err)
	}

	for _, key := range []string{"profileTypes", "types", "data"} {
		payload, ok := raw[key]
		if !ok {
			continue
		}

		var names []string
		if err := json.Unmarshal(payload, &names); err == nil {
			return names, nil
		}

		var typed []parcaProfileType
		if err := json.Unmarshal(payload, &typed); err == nil {
			names = make([]string, 0, len(typed))
			for _, t := range typed {
				if t.Name != "" {
					names = append(names, t.Name)
				}
			}

			return names, nil
		}
	}

	return nil, errors.New("could not find profile types field in response") //nolint:err113 // static parser sentinel
}

//nolint:err113 // parser reports exact wire-format validation failures.
func parseProfileTypeNamesProto(payload []byte) ([]string, error) {
	var names []string

	for len(payload) > 0 {
		num, wireType, n := protowire.ConsumeTag(payload)
		if n < 0 {
			return nil, fmt.Errorf("invalid profile types proto tag: %d", n)
		}

		payload = payload[n:]

		if num != 1 || wireType != protowire.BytesType {
			skip := protowire.ConsumeFieldValue(num, wireType, payload)
			if skip < 0 {
				return nil, fmt.Errorf("invalid profile types proto field: %d", skip)
			}

			payload = payload[skip:]

			continue
		}

		profileTypeMsg, m := protowire.ConsumeBytes(payload)
		if m < 0 {
			return nil, fmt.Errorf("invalid profile type entry: %d", m)
		}

		payload = payload[m:]

		name, err := parseProfileTypeEntryName(profileTypeMsg)
		if err != nil {
			return nil, err
		}

		if name != "" {
			names = append(names, name)
		}
	}

	return names, nil
}

//nolint:err113 // parser reports exact wire-format validation failures.
func parseProfileTypeEntryName(msg []byte) (string, error) {
	for len(msg) > 0 {
		num, wireType, n := protowire.ConsumeTag(msg)
		if n < 0 {
			return "", fmt.Errorf("invalid profile type entry tag: %d", n)
		}

		msg = msg[n:]

		if num == 1 && wireType == protowire.BytesType {
			nameBytes, m := protowire.ConsumeBytes(msg)
			if m < 0 {
				return "", fmt.Errorf("invalid profile type name: %d", m)
			}

			return string(nameBytes), nil
		}

		skip := protowire.ConsumeFieldValue(num, wireType, msg)
		if skip < 0 {
			return "", fmt.Errorf("invalid profile type field: %d", skip)
		}

		msg = msg[skip:]
	}

	return "", nil
}

//nolint:err113 // parser reports exact wire-format validation failures.
func parseQueryResponsePprofProto(payload []byte) ([]byte, error) {
	for len(payload) > 0 {
		num, wireType, n := protowire.ConsumeTag(payload)
		if n < 0 {
			return nil, fmt.Errorf("invalid query response tag: %d", n)
		}

		payload = payload[n:]

		if num == 6 && wireType == protowire.BytesType {
			pprofData, m := protowire.ConsumeBytes(payload)
			if m < 0 {
				return nil, fmt.Errorf("invalid pprof bytes field: %d", m)
			}

			return pprofData, nil
		}

		skip := protowire.ConsumeFieldValue(num, wireType, payload)
		if skip < 0 {
			return nil, fmt.Errorf("invalid query response field: %d", skip)
		}

		payload = payload[skip:]
	}

	return nil, errors.New("pprof field not found in query response")
}

//nolint:mnd // protobuf field numbers are wire-level schema values.
func buildQueryRequestProto(profileType string, now time.Time) []byte {
	startTimestamp := encodeTimestampProto(time.Unix(0, 0).UTC())
	endTimestamp := encodeTimestampProto(now.UTC())

	mergeProfile := []byte{}
	mergeProfile = protowire.AppendTag(mergeProfile, 1, protowire.BytesType)
	mergeProfile = protowire.AppendString(mergeProfile, profileType+"{}")
	mergeProfile = protowire.AppendTag(mergeProfile, 2, protowire.BytesType)
	mergeProfile = protowire.AppendBytes(mergeProfile, startTimestamp)
	mergeProfile = protowire.AppendTag(mergeProfile, 3, protowire.BytesType)
	mergeProfile = protowire.AppendBytes(mergeProfile, endTimestamp)

	queryRequest := []byte{}
	queryRequest = protowire.AppendTag(queryRequest, 1, protowire.VarintType)
	queryRequest = protowire.AppendVarint(queryRequest, 2) // QUERY_REQUEST_MODE_MERGE
	queryRequest = protowire.AppendTag(queryRequest, 3, protowire.BytesType)
	queryRequest = protowire.AppendBytes(queryRequest, mergeProfile)
	queryRequest = protowire.AppendTag(queryRequest, 5, protowire.VarintType)
	queryRequest = protowire.AppendVarint(queryRequest, 1) // QUERY_REQUEST_REPORT_TYPE_PPROF

	return queryRequest
}

//nolint:mnd,gosec // protobuf timestamp wire encoding uses fixed field tags and non-negative values.
func encodeTimestampProto(t time.Time) []byte {
	if t.Unix() < 0 {
		t = time.Unix(0, 0).UTC()
	}

	b := []byte{}
	b = protowire.AppendTag(b, 1, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(t.Unix()))
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(int64(t.Nanosecond())))

	return b
}

//nolint:err113,mnd,gosec // grpc-web framing and detailed errors are intentional.
func grpcWebUnary(ctx context.Context, client *http.Client, baseURL, path string, requestMessage []byte) ([]byte, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing Parca URL: %w", err)
	}

	parsedURL.Path = path

	framedReq := make([]byte, 0, 5+len(requestMessage))
	framedReq = append(framedReq, 0x00)
	lengthPrefix := make([]byte, 4)

	if len(requestMessage) > math.MaxUint32 {
		return nil, fmt.Errorf("grpc-web request too large: %d bytes", len(requestMessage))
	}

	binary.BigEndian.PutUint32(lengthPrefix, uint32(len(requestMessage)))
	framedReq = append(framedReq, lengthPrefix...)
	framedReq = append(framedReq, requestMessage...)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedURL.String(), bytes.NewReader(framedReq))
	if err != nil {
		return nil, fmt.Errorf("creating grpc-web request: %w", err)
	}

	req.Header.Set("Content-Type", "application/grpc-web+proto")
	req.Header.Set("Accept", "application/grpc-web+proto")
	req.Header.Set("X-Grpc-Web", "1")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending grpc-web request: %w", err)
	}
	defer closeResource("grpc-web response body", resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d from %s", ErrUnexpectedHTTPStatus, resp.StatusCode, path)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading grpc-web response: %w", err)
	}

	if looksLikeHTML(resp.Header.Get("Content-Type"), body) {
		return nil, fmt.Errorf("grpc-web endpoint returned HTML (likely SPA) from %s", path)
	}

	msgPayload, grpcStatus, grpcMsg, err := parseGRPCWebResponse(body)
	if err != nil {
		return nil, err
	}

	if grpcStatus != 0 {
		return nil, fmt.Errorf("grpc-web non-zero status %d from %s: %s", grpcStatus, path, grpcMsg)
	}

	return msgPayload, nil
}

//nolint:revive,err113,mnd,gocritic // grpc-web parser needs this return shape and wire constants.
func parseGRPCWebResponse(body []byte) ([]byte, int, string, error) {
	var messagePayload []byte

	grpcStatus := 0
	grpcMessage := ""

	for len(body) > 0 {
		if len(body) < 5 {
			return nil, 0, "", errors.New("invalid grpc-web frame: too short")
		}

		frameType := body[0]
		frameLen := binary.BigEndian.Uint32(body[1:5])
		body = body[5:]

		if uint64(len(body)) < uint64(frameLen) {
			return nil, 0, "", fmt.Errorf("invalid grpc-web frame length: want %d have %d", frameLen, len(body))
		}

		framePayload := body[:frameLen]
		body = body[frameLen:]

		if frameType&0x80 == 0x80 {
			status, msg := parseGRPCWebTrailer(framePayload)
			grpcStatus = status
			grpcMessage = msg

			continue
		}

		messagePayload = append(messagePayload, framePayload...)
	}

	return messagePayload, grpcStatus, grpcMessage, nil
}

//nolint:mnd,gocritic // trailer parsing uses fixed delimiter behavior and concise returns.
func parseGRPCWebTrailer(payload []byte) (int, string) {
	status := 0
	message := ""

	lines := strings.SplitSeq(string(payload), "\r\n")
	for line := range lines {
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch key {
		case "grpc-status":
			if parsed, err := strconv.Atoi(value); err == nil {
				status = parsed
			}
		case "grpc-message":
			message = value
		default:
		}
	}

	return status, message
}

func looksLikeHTML(contentType string, body []byte) bool {
	trimmed := strings.TrimSpace(strings.ToLower(string(body)))
	ctype := strings.ToLower(contentType)

	if strings.Contains(ctype, "text/html") {
		return true
	}

	return strings.HasPrefix(trimmed, "<!doctype html") || strings.HasPrefix(trimmed, "<html")
}

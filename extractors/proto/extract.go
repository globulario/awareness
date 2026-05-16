// Package proto extracts proto_service, rpc_method, proto_message, and authz
// annotation nodes from .proto files using line-based parsing.
package proto

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/awareness/graph"
)

// Extract walks walkDir for .proto files and extracts service/rpc/message nodes.
// Paths are stored relative to pathRoot (typically the repo root).
func Extract(ctx context.Context, g *graph.Graph, walkDir, pathRoot string) error {
	return filepath.WalkDir(walkDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".proto") {
			return nil
		}
		rel, err := filepath.Rel(pathRoot, path)
		if err != nil {
			return err
		}
		return extractProtoFile(ctx, g, path, rel)
	})
}

func extractProtoFile(ctx context.Context, g *graph.Graph, absPath, relPath string) error {
	f, err := os.Open(absPath)
	if err != nil {
		return nil // skip unreadable files
	}
	defer f.Close()

	fileID := "source_file:" + relPath
	_ = g.AddNode(ctx, graph.Node{
		ID:   fileID,
		Type: graph.NodeTypeSourceFile,
		Name: filepath.Base(relPath),
		Path: relPath,
	})

	var (
		packageName    string
		currentService string
		currentRPCID   string
		inAuthzBlock   bool
		authzAction    string
		authzResource  string
	)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Extract proto package name for qualifying service names.
		if strings.HasPrefix(line, "package ") && !strings.HasPrefix(line, "package_") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				packageName = strings.TrimSuffix(parts[1], ";")
			}
			continue
		}

		// Detect authz option block start (must be inside a service/RPC context).
		if strings.Contains(line, "option (globular.auth.authz)") {
			inAuthzBlock = true
			authzAction = ""
			authzResource = ""
			continue
		}

		// Parse authz block fields.
		if inAuthzBlock {
			if strings.Contains(line, "action:") {
				authzAction = extractQuotedOrBare(line, "action:")
			}
			if strings.Contains(line, "resource_type:") {
				authzResource = extractQuotedOrBare(line, "resource_type:")
			}
			// Closing brace ends the authz block.
			if line == "};" || line == "}" {
				inAuthzBlock = false
				// Create authz node and edge if we have an active RPC.
				if currentRPCID != "" && authzAction != "" {
					authzID := "authz:" + currentRPCID
					summary := authzAction
					if authzResource != "" {
						summary = fmt.Sprintf("action=%s resource_type=%s", authzAction, authzResource)
					}
					_ = g.AddNode(ctx, graph.Node{
						ID:      authzID,
						Type:    graph.NodeTypeAuthzAnnotation,
						Name:    authzAction,
						Summary: summary,
						Path:    relPath,
					})
					_ = g.AddEdge(ctx, graph.Edge{Src: currentRPCID, Kind: graph.EdgeHasAuthz, Dst: authzID})
				}
			}
			continue
		}

		switch {
		case strings.HasPrefix(line, "service "):
			// service Foo {
			name := extractProtoName(line)
			if name == "" {
				continue
			}
			// Use package-qualified name if package is known.
			qualifiedName := name
			if packageName != "" {
				qualifiedName = packageName + "." + name
			}
			currentService = qualifiedName
			svcID := "proto_service:" + qualifiedName
			_ = g.AddNode(ctx, graph.Node{
				ID:      svcID,
				Type:    graph.NodeTypeProtoService,
				Name:    qualifiedName,
				Path:    relPath,
				Summary: fmt.Sprintf("Proto service defined in %s", relPath),
			})
			_ = g.AddEdge(ctx, graph.Edge{Src: fileID, Kind: graph.EdgeDefines, Dst: svcID})

		case strings.HasPrefix(line, "rpc "):
			// rpc MethodName (Request) returns (Response) {}
			// rpc SaveFile(stream SaveFileRequest) returns (SaveFileResponse) {}
			methodName := extractRPCName(line)
			if methodName == "" || currentService == "" {
				continue
			}
			methodID := "rpc_method:" + currentService + "." + methodName
			currentRPCID = methodID

			// Detect streaming mode.
			clientStreaming := strings.Contains(line, "(stream ")
			serverStreaming := false
			if idx := strings.Index(line, "returns"); idx >= 0 {
				serverStreaming = strings.Contains(line[idx:], "(stream ")
			}
			streamMode := streamingMode(clientStreaming, serverStreaming)

			summary := methodName
			if streamMode != "" {
				summary = fmt.Sprintf("%s [%s]", methodName, streamMode)
			}

			_ = g.AddNode(ctx, graph.Node{
				ID:      methodID,
				Type:    graph.NodeTypeRPCMethod,
				Name:    methodName,
				Path:    relPath,
				Summary: summary,
			})
			svcID := "proto_service:" + currentService
			_ = g.AddEdge(ctx, graph.Edge{Src: svcID, Kind: graph.EdgeOwns, Dst: methodID})

			// Streaming edge.
			if streamMode != "" {
				streamNodeID := "streaming_mode:" + streamMode
				_ = g.AddNode(ctx, graph.Node{
					ID:   streamNodeID,
					Type: graph.NodeTypeStreamingMode,
					Name: streamMode,
				})
				_ = g.AddEdge(ctx, graph.Edge{Src: methodID, Kind: graph.EdgeHasStreamingMode, Dst: streamNodeID})
			}

			// Link request/response message types.
			req, resp := extractRPCMessages(line)
			for _, msg := range []string{req, resp} {
				if msg == "" {
					continue
				}
				// Strip stream keyword from message names.
				msg = strings.TrimPrefix(msg, "stream ")
				msg = strings.TrimSpace(msg)
				if msg == "" {
					continue
				}
				msgID := "proto_message:" + msg
				_ = g.AddNode(ctx, graph.Node{
					ID:   msgID,
					Type: graph.NodeTypeProtoMessage,
					Name: msg,
					Path: relPath,
				})
				_ = g.AddEdge(ctx, graph.Edge{Src: methodID, Kind: graph.EdgeRequires, Dst: msgID})
			}

		case strings.HasPrefix(line, "message "):
			// message Foo {
			name := extractProtoName(line)
			if name == "" {
				continue
			}
			msgID := "proto_message:" + name
			_ = g.AddNode(ctx, graph.Node{
				ID:   msgID,
				Type: graph.NodeTypeProtoMessage,
				Name: name,
				Path: relPath,
			})
			_ = g.AddEdge(ctx, graph.Edge{Src: fileID, Kind: graph.EdgeDefines, Dst: msgID})

		case line == "}" && currentService != "" && !inAuthzBlock:
			// Closing brace of a service or RPC body.
			// If we just closed an RPC block (currentRPCID was set and we see }),
			// clear the current RPC so authz block doesn't leak across RPCs.
			// Note: this is a rough heuristic — sufficient for V1.
			if currentRPCID != "" {
				currentRPCID = ""
			} else {
				currentService = ""
			}
		}
	}
	return scanner.Err()
}

// streamingMode returns a human-readable streaming classification.
func streamingMode(client, server bool) string {
	switch {
	case client && server:
		return "bidirectional_streaming"
	case client:
		return "client_streaming"
	case server:
		return "server_streaming"
	default:
		return ""
	}
}

// extractQuotedOrBare extracts the value after a key in an option line.
// Handles: key: "value", key: value, key: 'value'
func extractQuotedOrBare(line, key string) string {
	idx := strings.Index(line, key)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[idx+len(key):])
	rest = strings.TrimSuffix(rest, ",")
	rest = strings.TrimSuffix(rest, ";")
	rest = strings.Trim(rest, `"'`)
	return strings.TrimSpace(rest)
}

// extractProtoName extracts the identifier from "service Foo {" or "message Foo {".
func extractProtoName(line string) string {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSuffix(parts[1], "{")
}

// extractRPCName extracts the method name from "rpc MethodName (...) returns (...) {}".
// Handles both "rpc Method (" and "rpc Method(" (no space before paren).
func extractRPCName(line string) string {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return ""
	}
	name := parts[1]
	// Strip everything from "(" onward — handles both "Method(" and "Method (".
	if idx := strings.Index(name, "("); idx >= 0 {
		name = name[:idx]
	}
	return name
}

// extractRPCMessages extracts request and response message types (stripping "stream").
func extractRPCMessages(line string) (req, resp string) {
	// rpc Foo (ReqType) returns (RespType) {}
	// rpc SaveFile(stream SaveFileRequest) returns (SaveFileResponse) {}
	open := strings.Index(line, "(")
	close := strings.Index(line, ")")
	if open < 0 || close < 0 || close <= open {
		return "", ""
	}
	req = strings.TrimSpace(line[open+1 : close])
	req = strings.TrimPrefix(req, "stream ")
	req = strings.TrimSpace(req)

	rest := line[close+1:]
	retIdx := strings.Index(rest, "returns")
	if retIdx < 0 {
		return req, ""
	}
	rest = rest[retIdx+len("returns"):]
	open2 := strings.Index(rest, "(")
	close2 := strings.Index(rest, ")")
	if open2 < 0 || close2 < 0 || close2 <= open2 {
		return req, ""
	}
	resp = strings.TrimSpace(rest[open2+1 : close2])
	resp = strings.TrimPrefix(resp, "stream ")
	resp = strings.TrimSpace(resp)
	return req, resp
}

package hub

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/henrygd/beszel/internal/hub/systems"
)

// agentEgressHandler associates a source address observed at the Northflank
// public HTTP ingress with the explicitly configured system.
func agentEgressHandler(systemID string) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		tokens := e.Request.Header.Values("X-Token")
		if len(tokens) != 1 || tokens[0] == "" || len(tokens[0]) > 64 {
			return e.UnauthorizedError("Invalid agent token.", nil)
		}
		token := tokens[0]
		if _, universal := universalTokenMap.GetMap().GetOk(token); universal {
			return e.UnauthorizedError("Invalid agent token.", nil)
		}
		ip := parseNorthflankEgressIP(e.Request.Header.Values("X-Envoy-External-Address"))
		if !ip.IsValid() {
			return e.BadRequestError("Public ingress IP is unavailable.", nil)
		}
		err := e.App.RunInTransaction(func(tx core.App) error {
			matches, err := tx.FindRecordsByFilter("fingerprints", "token = {:token}", "", 2, 0, dbx.Params{"token": token})
			if err != nil {
				return err
			}
			if len(matches) != 1 || matches[0].GetString("system") != systemID || matches[0].GetString("fingerprint") == "" {
				return e.UnauthorizedError("Invalid agent token.", nil)
			}
			universal, err := tx.CountRecords("universal_tokens", dbx.HashExp{"token": token})
			if err != nil {
				return err
			}
			if universal != 0 {
				return e.UnauthorizedError("Invalid agent token.", nil)
			}
			record, err := tx.FindRecordById("systems", systemID)
			if errors.Is(err, sql.ErrNoRows) {
				return e.UnauthorizedError("Invalid agent token.", nil)
			}
			if err != nil {
				return err
			}
			return saveSystemEgressIP(e.Request.Context(), tx, record, ip)
		})
		if err != nil {
			return err
		}
		return e.NoContent(http.StatusNoContent)
	}
}

func (h *Hub) recordSandboxEgressIP(systemID string, ip netip.Addr) {
	// The WebSocket handshake has completed and the HTTP request may have ended.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := h.RunInTransaction(func(tx core.App) error {
		record, err := tx.FindRecordById("systems", systemID)
		if err != nil {
			return err
		}
		if record.GetString("host") != systems.WebSocketOnlyHost {
			return nil
		}
		return saveSystemEgressIP(ctx, tx, record, ip)
	})
	if err != nil {
		h.Logger().Warn("Failed to update sandbox egress IP", "system", systemID, "error", err)
	}
}

func saveSystemEgressIP(ctx context.Context, app core.App, record *core.Record, ip netip.Addr) error {
	address := ip.String()
	if record.GetString("ip") == address {
		return nil
	}
	record.IgnoreUnchangedFields(true)
	record.Set("ip", address)
	return app.SaveWithContext(ctx, record)
}

var (
	globalEgressIPv6 = netip.MustParsePrefix("2000::/3")
	nonPublicEgress  = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("3fff::/20"),
	}
)

// Northflank replaces this header on public requests. Do not fall back to
// client-supplied forwarding headers, request data or the internal proxy address.
func parseNorthflankEgressIP(values []string) netip.Addr {
	if len(values) != 1 {
		return netip.Addr{}
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(values[0]))
	if err != nil || ip.Zone() != "" {
		return netip.Addr{}
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !globalEgressIPv6.Contains(ip)) {
		return netip.Addr{}
	}
	for _, prefix := range nonPublicEgress {
		if prefix.Contains(ip) {
			return netip.Addr{}
		}
	}
	return ip
}

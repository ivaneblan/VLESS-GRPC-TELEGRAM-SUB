package links

import (
	"fmt"
	"net/url"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
)

type ServerParams struct {
	Host        string
	Port        int
	SNI         string
	Flow        string
	PBK         string
	SID         string
	DisplayName string
	GRPCService string
	XHTTPPort   int
	XHTTPPath   string
	XHTTPMode   string
	FPDesktop   string
	FPMobile    string
}

func BuildServerLinks(p ServerParams, uuid string) map[string]string {
	name := p.DisplayName
	if name == "" {
		name = p.Host
	}
	return map[string]string{
		"default": buildGRPC(p, uuid, name, p.FPDesktop),
		"mobile":  buildGRPC(p, uuid, name+"-mobile", p.FPMobile),
		"grpc":    buildGRPC(p, uuid, name+"-grpc", p.FPDesktop),
		"xhttp":   buildXHTTP(p, uuid, name+"-xhttp", p.FPDesktop),
		"tcp":     buildTCP(p, uuid, name+"-tcp", p.FPDesktop),
		"direct":  buildGRPC(p, uuid, name+"-Direct", p.FPDesktop),
	}
}

// ParamsFromConfig builds link parameters for a server. A bridge node
// (relay_to set) terminates the client's Reality session locally, so it uses
// its own Reality keys and host exactly like an exit; only the display label is
// annotated with the downstream exit for clarity.
func ParamsFromConfig(cfg *config.Config, sec *config.Secrets, server *config.ServerDef) (ServerParams, error) {
	x := cfg.Xray.WithDefaults()
	reality := sec.Reality(server.ID)
	if reality.PublicKey == "" || reality.ShortID == "" {
		return ServerParams{}, fmt.Errorf("missing reality keys for %s", server.ID)
	}
	displayName := server.Name
	if server.IsBridge() {
		if displayName == "" {
			displayName = server.Host
		}
		if exit := cfg.ExitForBridge(server); exit != nil && exit.Name != "" {
			displayName = fmt.Sprintf("%s via %s", exit.Name, server.Name)
		}
	}
	return ServerParams{
		Host:        server.Host,
		Port:        x.Port,
		SNI:         x.SNI,
		Flow:        x.Flow,
		PBK:         reality.PublicKey,
		SID:         reality.ShortID,
		DisplayName: displayName,
		GRPCService: x.GRPCServiceName,
		XHTTPPort:   x.XHTTPPort,
		XHTTPPath:   x.XHTTPPath,
		XHTTPMode:   x.XHTTPMode,
		FPDesktop:   x.FPDesktop,
		FPMobile:    x.FPMobile,
	}, nil
}

func buildGRPC(p ServerParams, uuid, label, fp string) string {
	svc := url.QueryEscape(p.GRPCService)
	return fmt.Sprintf(
		"vless://%s@%s:%d?encryption=none&security=reality&sni=%s&fp=%s&pbk=%s&sid=%s&type=grpc&serviceName=%s#%s",
		uuid, p.Host, p.Port, p.SNI, fp, p.PBK, p.SID, svc, url.QueryEscape(label),
	)
}

func buildTCP(p ServerParams, uuid, label, fp string) string {
	return fmt.Sprintf(
		"vless://%s@%s:%d?encryption=none&flow=%s&security=reality&sni=%s&fp=%s&pbk=%s&sid=%s&type=tcp#%s",
		uuid, p.Host, p.Port, p.Flow, p.SNI, fp, p.PBK, p.SID, url.QueryEscape(label),
	)
}

func buildXHTTP(p ServerParams, uuid, label, fp string) string {
	path := url.QueryEscape(p.XHTTPPath)
	return fmt.Sprintf(
		"vless://%s@%s:%d?encryption=none&security=reality&sni=%s&fp=%s&pbk=%s&sid=%s&type=xhttp&path=%s&mode=%s#%s",
		uuid, p.Host, p.XHTTPPort, p.SNI, fp, p.PBK, p.SID, path, p.XHTTPMode, url.QueryEscape(label),
	)
}

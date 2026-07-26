package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// subInfo is the JSON payload behind the public subscription page.
type subInfoResponse struct {
	NodeName    string          `json:"node_name"`
	Status      string          `json:"status"`
	QuotaBytes  int64           `json:"quota_bytes"`
	UsedBytes   int64           `json:"used_bytes"`
	RemainBytes int64           `json:"remaining_bytes"`
	EndAt       int64           `json:"end_at"`
	DaysLeft    int64           `json:"days_left"`
	GRPCAddress string          `json:"grpc_address"`
	Protocol    string          `json:"protocol"`
	CertPEM     string          `json:"cert_pem"`
	APIKey      string          `json:"api_key"`
	Inbounds    json.RawMessage `json:"inbounds"`
}

// loadSubInfoByToken builds the public info for a subscription token. The node
// is contacted for the live inbounds; usage/expiry come from the panel DB.
func (a *API) loadSubInfoByToken(ctx context.Context, token string) (*subInfoResponse, bool) {
	sub, err := a.store.GetSubscriptionByToken(token)
	if err != nil {
		return nil, false
	}
	info := &subInfoResponse{
		Status:     sub.Status,
		QuotaBytes: sub.QuotaBytes,
		UsedBytes:  sub.UsedBytes,
		EndAt:      sub.EndAt,
		Protocol:   "grpc",
		APIKey:     sub.APIKey,
		Inbounds:   json.RawMessage(`{"inbounds":[]}`),
	}
	if r := sub.QuotaBytes - sub.UsedBytes; r > 0 {
		info.RemainBytes = r
	}
	now := time.Now().Unix()
	if sub.EndAt > now {
		info.DaysLeft = (sub.EndAt - now) / 86400
	}
	if sub.EndAt > 0 && now >= sub.EndAt {
		info.Status = "expired"
	}
	if node, err := a.store.GetNode(sub.NodeID); err == nil {
		info.NodeName = node.Name
		info.CertPEM = node.CertPEM
		info.GRPCAddress = grpcAddressFor(node.Address, node.GRPCPort)
		cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		info.Inbounds = a.resolveInbounds(cctx, node)
	}
	return info, true
}

// subInfo serves the public subscription info as JSON (no auth; token-gated).
//
// The body carries the node API key, so it must never be cached: without an
// explicit no-store any intermediary — corporate proxy, CDN, browser history —
// may retain a live credential. The HTML page already sets this.
func (a *API) subInfo(w http.ResponseWriter, r *http.Request) {
	info, ok := a.loadSubInfoByToken(r.Context(), chi.URLParam(r, "token"))
	if !ok {
		writeError(w, http.StatusNotFound, "subscription not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	writeJSON(w, http.StatusOK, info)
}

// subPage serves a self-contained HTML page for the customer: the node config
// to paste into their panel's core, plus quota/expiry status (no auth).
func (a *API) subPage(w http.ResponseWriter, r *http.Request) {
	info, ok := a.loadSubInfoByToken(r.Context(), chi.URLParam(r, "token"))
	if !ok {
		http.Error(w, "subscription not found", http.StatusNotFound)
		return
	}

	var inboundsPretty bytes.Buffer
	if json.Indent(&inboundsPretty, info.Inbounds, "", "  ") != nil {
		inboundsPretty.Write(info.Inbounds)
	}

	pct := 0
	if info.QuotaBytes > 0 {
		pct = int(info.UsedBytes * 100 / info.QuotaBytes)
		if pct > 100 {
			pct = 100
		}
	}
	expiry := "—"
	if info.EndAt > 0 {
		expiry = time.Unix(info.EndAt, 0).Format("2006-01-02")
	}

	data := subPageData{
		NodeName:    info.NodeName,
		Status:      info.Status,
		StatusFa:    statusFa(info.Status),
		Used:        humanBytes(info.UsedBytes),
		Quota:       humanBytes(info.QuotaBytes),
		Remaining:   humanBytes(info.RemainBytes),
		Pct:         pct,
		Expiry:      expiry,
		DaysLeft:    info.DaysLeft,
		GRPCAddress: info.GRPCAddress,
		Protocol:    info.Protocol,
		APIKey:      info.APIKey,
		CertPEM:     info.CertPEM,
		Inbounds:    inboundsPretty.String(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_ = subPageTmpl.Execute(w, data)
}

type subPageData struct {
	NodeName, Status, StatusFa                       string
	Used, Quota, Remaining                           string
	Pct                                              int
	Expiry                                           string
	DaysLeft                                         int64
	GRPCAddress, Protocol, APIKey, CertPEM, Inbounds string
}

func statusFa(s string) string {
	switch s {
	case "active":
		return "فعال"
	case "suspended":
		return "معلق"
	case "expired":
		return "منقضی"
	}
	return s
}

func humanBytes(n int64) string {
	if n <= 0 {
		return "0"
	}
	const u = "BKMGT"
	f := float64(n)
	i := 0
	for f >= 1024 && i < 4 {
		f /= 1024
		i++
	}
	unit := string(u[i])
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %sB", f, unit)
}

var subPageTmpl = template.Must(template.New("sub").Parse(subPageHTML))

const subPageHTML = `<!doctype html>
<html lang="fa" dir="rtl">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>اشتراک {{.NodeName}}</title>
<style>
  :root{--bg:#0f172a;--card:#1e293b;--mut:#94a3b8;--txt:#e2e8f0;--ac:#6366f1;--ok:#22c55e;--warn:#f59e0b;--danger:#ef4444}
  *{box-sizing:border-box}
  body{margin:0;background:var(--bg);color:var(--txt);font-family:system-ui,Segoe UI,Tahoma,sans-serif;line-height:1.8}
  .wrap{max-width:760px;margin:0 auto;padding:20px}
  .card{background:var(--card);border-radius:16px;padding:20px;margin:16px 0;box-shadow:0 6px 24px rgba(0,0,0,.25)}
  h1{font-size:20px;margin:8px 0}
  h2{font-size:15px;color:var(--mut);margin:0 0 12px;font-weight:600}
  .badge{display:inline-block;padding:3px 12px;border-radius:999px;font-size:13px}
  .active{background:rgba(34,197,94,.15);color:var(--ok)} .expired{background:rgba(239,68,68,.15);color:var(--danger)} .suspended{background:rgba(245,158,11,.15);color:var(--warn)}
  .bar{background:#0b1220;border-radius:999px;height:12px;overflow:hidden;margin:8px 0}
  .bar > i{display:block;height:100%;background:linear-gradient(90deg,#6366f1,#22c55e)}
  .row{display:flex;flex-wrap:wrap;gap:16px}
  .row > div{flex:1;min-width:140px}
  .lbl{color:var(--mut);font-size:13px} .val{font-size:16px;font-weight:600}
  textarea{width:100%;background:#0b1220;color:#cbd5e1;border:1px solid #334155;border-radius:10px;padding:10px;font-family:ui-monospace,monospace;font-size:12.5px;direction:ltr}
  .field{margin:12px 0}
  input.copy{width:100%;background:#0b1220;color:#cbd5e1;border:1px solid #334155;border-radius:10px;padding:10px;direction:ltr;font-family:ui-monospace,monospace}
  button{background:var(--ac);color:#fff;border:0;border-radius:10px;padding:8px 14px;cursor:pointer;font-size:13px;margin-top:6px}
  .muted{color:var(--mut);font-size:13px}
</style>
</head>
<body>
<div class="wrap">
  <div class="card">
    <h1>اشتراک نود {{.NodeName}}</h1>
    <span class="badge {{.Status}}">{{.StatusFa}}</span>
    <div class="bar"><i style="width:{{.Pct}}%"></i></div>
    <div class="row">
      <div><div class="lbl">مصرف</div><div class="val">{{.Used}} / {{.Quota}}</div></div>
      <div><div class="lbl">باقی‌مانده</div><div class="val">{{.Remaining}}</div></div>
      <div><div class="lbl">انقضا</div><div class="val">{{.Expiry}} ({{.DaysLeft}} روز)</div></div>
    </div>
  </div>

  <div class="card">
    <h2>این مقادیر را در نود پنل PasarGuard خودت وارد کن</h2>
    <div class="field">
      <div class="lbl">آدرس gRPC</div>
      <input class="copy" id="addr" readonly value="{{.GRPCAddress}}"/>
      <button onclick="cp('addr')">کپی آدرس</button>
    </div>
    <div class="field">
      <div class="lbl">کلید API (هنگام افزودن نود در پنل وارد کن)</div>
      <input class="copy" id="apikey" readonly value="{{.APIKey}}"/>
      <button onclick="cp('apikey')">کپی کلید</button>
    </div>
    <div class="field">
      <div class="lbl">پروتکل</div>
      <input class="copy" readonly value="{{.Protocol}}"/>
    </div>
    <div class="field">
      <div class="lbl">گواهی نود (Certificate)</div>
      <textarea id="cert" rows="5" readonly>{{.CertPEM}}</textarea>
      <button onclick="cp('cert')">کپی گواهی</button>
    </div>
    <div class="field">
      <div class="lbl">کانفیگ inbound هسته (دقیقاً همین را در هسته‌ی پنلت بگذار)</div>
      <textarea id="inb" rows="12" readonly>{{.Inbounds}}</textarea>
      <button onclick="cp('inb')">کپی کانفیگ</button>
    </div>
    <p class="muted">کلید API مشتری بالا را هنگام افزودن نود در پنل PasarGuard وارد کن.</p>
  </div>
</div>
<script>
function cp(id){var e=document.getElementById(id);e.select();if(navigator.clipboard)navigator.clipboard.writeText(e.value);}
</script>
</body>
</html>`

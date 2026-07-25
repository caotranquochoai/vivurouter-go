package dashboard

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/local/vivurouter-go/internal/store"
)

func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	h.render(w, r, "dashboard.html", "dashboard.title")
}

func (h *Handlers) ChatStudioPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	h.render(w, r, "chat_studio.html", "chat_studio.title")
}

func (h *Handlers) MediaPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	h.render(w, r, "media.html", "media.title")
}

func (h *Handlers) ProvidersPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if providerID := strings.TrimSpace(r.URL.Query().Get("provider")); providerID != "" && r.Method == http.MethodGet {
		http.Redirect(w, r, "/providers/"+url.PathEscape(providerID), http.StatusFound)
		return
	}
	if r.Method == http.MethodPost {
		switch strings.ToLower(strings.TrimSpace(r.FormValue("action"))) {
		case "delete":
			h.deleteProviderForm(w, r)
		case "toggle":
			h.toggleProviderForm(w, r)
		case "add-models":
			h.addProviderModelsForm(w, r)
		default:
			h.saveProviderForm(w, r)
		}
		return
	}
	h.render(w, r, "providers.html", "providers.title")
}

func (h *Handlers) ProxiesPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		switch strings.ToLower(strings.TrimSpace(r.FormValue("action"))) {
		case "delete-proxy":
			h.deleteProxyForm(w, r)
		case "bulk-proxies":
			h.bulkProxiesForm(w, r)
		default:
			h.saveProxyForm(w, r)
		}
		return
	}
	h.render(w, r, "proxies.html", "proxies.title")
}

func (h *Handlers) ProviderDetailPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	providerID, ok := providerIDFromDetailPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if providerID == "providers" {
		http.Redirect(w, r, "/providers?saved=1", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodPost {
		switch strings.ToLower(strings.TrimSpace(r.FormValue("action"))) {
		case "delete":
			h.deleteProviderForm(w, r)
		case "toggle":
			h.toggleProviderForm(w, r)
		case "add-models":
			h.addProviderModelsForm(w, r)
		case "add-key":
			h.addKeyForm(w, r)
		case "bulk-keys":
			h.bulkKeysForm(w, r)
		case "delete-key":
			h.deleteKeyForm(w, r)
		case "save-keys-config":
			h.saveKeysConfigForm(w, r)
		default:
			h.saveProviderForm(w, r)
		}
		return
	}
	if _, found, err := h.store.GetProvider(providerID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if !found {
		http.NotFound(w, r)
		return
	}
	renderReq := r.Clone(r.Context())
	urlCopy := *r.URL
	query := urlCopy.Query()
	query.Set("provider", providerID)
	urlCopy.RawQuery = query.Encode()
	renderReq.URL = &urlCopy
	h.render(w, renderReq, "provider_detail.html", "providers.title")
}

func providerIDFromDetailPath(path string) (string, bool) {
	id := strings.TrimPrefix(path, "/providers/")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	decoded, err := url.PathUnescape(id)
	if err != nil || strings.TrimSpace(decoded) == "" {
		return "", false
	}
	return decoded, true
}

func providerFormRedirect(r *http.Request) string {
	if id, ok := providerIDFromDetailPath(r.URL.Path); ok {
		return "/providers/" + url.PathEscape(id) + "?saved=1"
	}
	return "/providers?saved=1"
}

func (h *Handlers) RequestsPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	h.render(w, r, "requests.html", "requests.title")
}

func (h *Handlers) PromptRoutersPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		settings, err := h.store.GetSettings()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		action := strings.ToLower(strings.TrimSpace(r.FormValue("action")))
		name := strings.TrimSpace(r.FormValue("name"))
		kept := []store.PromptRouter{}
		for _, existing := range settings.PromptRouters {
			if existing.Name != name {
				kept = append(kept, existing)
			}
		}
		if action != "delete" {
			router := store.PromptRouter{
				Name:                     name,
				Enabled:                  r.FormValue("enabled") == "on",
				ClassifierModel:          strings.TrimSpace(r.FormValue("classifier_model")),
				FallbackTarget:           strings.TrimSpace(r.FormValue("fallback_target")),
				FallbackRole:             strings.ToLower(strings.TrimSpace(r.FormValue("fallback_role"))),
				Description:              strings.TrimSpace(r.FormValue("description")),
				UseRawPrompt:             r.FormValue("use_raw_prompt") == "on",
				ClassifierPromptTemplate: strings.TrimSpace(r.FormValue("classifier_prompt_template")),
			}
			if router.FallbackTarget == "" {
				router.FallbackTarget = router.ClassifierModel
			}
			if router.FallbackRole == "" {
				router.FallbackRole = "planner"
			}
			roleCount, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("role_count")))
			if roleCount < 5 {
				roleCount = 5
			}
			if roleCount > 100 {
				roleCount = 100
			}
			for i := 1; i <= roleCount; i++ {
				suffix := strconv.Itoa(i)
				role := strings.ToLower(strings.TrimSpace(r.FormValue("role_" + suffix)))
				complexity := strings.ToLower(strings.TrimSpace(r.FormValue("complexity_" + suffix)))
				risk := strings.ToLower(strings.TrimSpace(r.FormValue("risk_" + suffix)))
				target := strings.TrimSpace(r.FormValue("target_" + suffix))
				if role != "" && target == "" {
					target = router.FallbackTarget
				}
				if role == "" && target == "" {
					continue
				}
				router.Routes = append(router.Routes, store.PromptRoute{
					Role:              role,
					Complexity:        complexity,
					Risk:              risk,
					Target:            target,
					InjectInstruction: r.FormValue("inject_"+suffix) == "on",
					Instruction:       strings.TrimSpace(r.FormValue("instruction_" + suffix)),
				})
			}
			kept = append(kept, router)
		}
		settings.PromptRouters = store.NormalizePromptRouters(kept)
		if err := h.store.SaveSettings(settings); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		http.Redirect(w, r, "/prompt-routers?saved=1", http.StatusFound)
		return
	}
	h.render(w, r, "prompt_routers.html", "prompt_routers.title")
}

func (h *Handlers) SettingsPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		h.saveSettingsForm(w, r)
		return
	}
	h.render(w, r, "settings.html", "settings.title")
}

func (h *Handlers) APIKeysPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		h.saveAPIKeysForm(w, r)
		return
	}
	h.render(w, r, "api_keys.html", "api_keys.title")
}

func (h *Handlers) PricingPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		h.savePricingForm(w, r)
		return
	}
	h.render(w, r, "pricing.html", "pricing.title")
}

func (h *Handlers) DataPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	h.render(w, r, "data.html", "Data")
}

func (h *Handlers) InvoicesPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	h.render(w, r, "invoices.html", "Invoices")
}

func (h *Handlers) CombosPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		h.saveCombosForm(w, r)
		return
	}
	h.render(w, r, "combos.html", "combos.title")
}

func (h *Handlers) GuardrailsPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		h.saveGuardrailsForm(w, r)
		return
	}
	h.render(w, r, "guardrails.html", "Guardrails")
}

func (h *Handlers) FusionsPage(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		h.saveFusionsForm(w, r)
		return
	}
	h.render(w, r, "fusions.html", "Fusion")
}

func (h *Handlers) AdminLoginPage(w http.ResponseWriter, r *http.Request) {
	settings, err := h.store.GetSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !settings.AdminSecurityEnabled || strings.TrimSpace(settings.AdminPasscode) == "" {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	if r.Method == http.MethodPost {
		if subtle.ConstantTimeCompare([]byte(r.FormValue("passcode")), []byte(settings.AdminPasscode)) == 1 {
			h.setAdminSession(w)
			http.Redirect(w, r, "/dashboard", http.StatusFound)
			return
		}
		h.renderLogin(w, "Mã bảo mật không đúng")
		return
	}
	h.renderLogin(w, "")
}

func (h *Handlers) AdminLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "admin_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/admin/login", http.StatusFound)
}

func (h *Handlers) renderLogin(w http.ResponseWriter, errText string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	bundle := translationBundle("vi")
	_ = h.templates.ExecuteTemplate(w, "login.html", map[string]any{"Title": translate(bundle, "login.title"), "Lang": "vi", "T": bundle, "Error": errText})
}

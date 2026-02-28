import { createApp, computed, onMounted, reactive, ref, watch, nextTick } from "https://unpkg.com/vue@3/dist/vue.esm-browser.js";
import jsyaml from "https://cdn.jsdelivr.net/npm/js-yaml@4/+esm";

// UUID polyfill for HTTP connections (crypto.randomUUID requires HTTPS)
function generateUUID() {
  if (crypto.randomUUID) {
    return crypto.randomUUID();
  }
  // Fallback for HTTP
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
    const r = Math.random() * 16 | 0;
    const v = c === 'x' ? r : (r & 0x3 | 0x8);
    return v.toString(16);
  });
}

const typeIcons = {
  openapi: "simple-icons:openapiinitiative",
  swagger2: "simple-icons:swagger",
  graphql: "simple-icons:graphql",
  wsdl: "mdi:xml",
  odata: "mdi:database-search",
  postman: "simple-icons:postman",
  openrpc: "mdi:code-json",
  grpc: "mdi:server-network",
  "jira-rest": "simple-icons:jira",
  asyncapi: "simple-icons:asyncapi",
  raml: "mdi:code-braces",
  apiblueprint: "mdi:file-document-outline",
  insomnia: "simple-icons:insomnia",
};

const typeLabels = {
  openapi: "OpenAPI",
  swagger2: "Swagger 2.0",
  graphql: "GraphQL",
  wsdl: "WSDL (SOAP)",
  odata: "OData v4",
  postman: "Postman Collection",
  openrpc: "OpenRPC (JSON-RPC)",
  grpc: "gRPC",
  "jira-rest": "Jira REST",
  asyncapi: "AsyncAPI",
  raml: "RAML",
  apiblueprint: "API Blueprint",
  insomnia: "Insomnia Collection",
};

// Known service identification — overlays the generic spec-type icon/label
const serviceIcons = {
  kubernetes: "simple-icons:kubernetes",
  gitlab:     "simple-icons:gitlab",
  jira:       "simple-icons:jira",
  slack:      "simple-icons:slack",
  gmail:      "simple-icons:gmail",
};

const serviceLabels = {
  kubernetes: "Kubernetes",
  gitlab:     "GitLab",
  jira:       "Jira",
  slack:      "Slack",
  gmail:      "Gmail",
};

/**
 * Identify well-known services from URL patterns.
 * specType comes from the backend detector (e.g. "jira-rest").
 */
function inferKnownService(baseUrl, specUrl, specType) {
  if (specType === "jira-rest") return "jira";
  const combined = (baseUrl || "") + "|" + (specUrl || "");
  if (/\/openapi\/v[23]|kubernetes\.default\.svc|:\d{4,5}\/api(s)?\//.test(combined)) return "kubernetes";
  if (/gitlab\.com|\/api\/v4\//.test(combined)) return "gitlab";
  if (/slack\.com|api\.slack\.com/.test(combined)) return "slack";
  if (/gmail\.googleapis\.com/.test(combined)) return "gmail";
  return "";
}

const apiClient = {
  async listProfiles() {
    const res = await fetch("/profiles");
    if (!res.ok) throw new Error(`List failed (${res.status})`);
    return res.json();
  },
  async loadProfile(name, token) {
    const res = await fetch(`/profiles/${encodeURIComponent(name)}?format=json`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!res.ok) throw new Error(`Load failed (${res.status})`);
    return res.json();
  },
  async saveProfile(name, token, config) {
    const res = await fetch(`/profiles/${encodeURIComponent(name)}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify({ token, config_json: config }),
    });
    if (!res.ok) {
      const msg = await res.text();
      throw new Error(`Save failed (${res.status}): ${msg}`);
    }
    return res.json();
  },
  async deleteProfile(name, token) {
    const res = await fetch(`/profiles/${encodeURIComponent(name)}`, {
      method: "DELETE",
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!res.ok) throw new Error(`Delete failed (${res.status})`);
    return res.json();
  },
  async detect(baseUrl, bearerToken) {
    const body = { base_url: baseUrl };
    if (bearerToken) body.bearer_token = bearerToken;
    const res = await fetch("/detect", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      const msg = await res.text();
      throw new Error(`Detect failed (${res.status}): ${msg}`);
    }
    return res.json();
  },
  async testSpec(specUrl) {
    const res = await fetch("/test", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ spec_url: specUrl }),
    });
    if (!res.ok) {
      const msg = await res.text();
      throw new Error(`Test failed (${res.status}): ${msg}`);
    }
    return res.json();
  },
  async fetchOperations(specUrl, specType, name) {
    const res = await fetch("/operations", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ spec_url: specUrl, spec_type: specType, name: name || "" }),
    });
    if (!res.ok) {
      const msg = await res.text();
      throw new Error(`Fetch operations failed (${res.status}): ${msg}`);
    }
    return res.json();
  },
};

// Profile export/import crypto — browser-side AES-256-GCM + PBKDF2
const profileCrypto = {
  toBase64url(buf) {
    return btoa(String.fromCharCode(...new Uint8Array(buf)))
      .replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
  },
  fromBase64url(str) {
    const b64 = str.replace(/-/g, '+').replace(/_/g, '/');
    const pad = b64.length % 4 ? b64 + '='.repeat(4 - b64.length % 4) : b64;
    const bin = atob(pad);
    const buf = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) buf[i] = bin.charCodeAt(i);
    return buf.buffer;
  },
  async deriveKey(password, saltBuf) {
    const keyMaterial = await crypto.subtle.importKey(
      'raw', new TextEncoder().encode(password), 'PBKDF2', false, ['deriveKey']
    );
    return crypto.subtle.deriveKey(
      { name: 'PBKDF2', salt: saltBuf, iterations: 200000, hash: 'SHA-256' },
      keyMaterial,
      { name: 'AES-GCM', length: 256 },
      false,
      ['encrypt', 'decrypt']
    );
  },
  async encryptApis(apis, password) {
    const salt = crypto.getRandomValues(new Uint8Array(16));
    const iv   = crypto.getRandomValues(new Uint8Array(12));
    const key  = await this.deriveKey(password, salt.buffer);
    const plain = new TextEncoder().encode(JSON.stringify(apis));
    const cipher = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, plain);
    return { salt: this.toBase64url(salt), iv: this.toBase64url(iv), payload: this.toBase64url(cipher) };
  },
  async decryptApis(encObj, password) {
    const salt = this.fromBase64url(encObj.salt);
    const iv   = new Uint8Array(this.fromBase64url(encObj.iv));
    const ct   = this.fromBase64url(encObj.payload);
    const key  = await this.deriveKey(password, salt);
    const plain = await crypto.subtle.decrypt({ name: 'AES-GCM', iv }, key, ct);
    return JSON.parse(new TextDecoder().decode(plain));
  },
};

function blankApi() {
  return {
    id: generateUUID(),
    name: "",
    baseUrl: "",
    specUrl: "",
    type: "",
    status: "",
    detectedOptions: [],
    authType: "none",
    bearerToken: "",
    basicUser: "",
    basicPass: "",
    apiKeyHeader: "X-API-Key",
    apiKeyValue: "",
    detectedOnce: false,
    // Response truncation (per-API)
    maxResponseBytes: "",
    // Filter configuration
    filterMode: "",
    filterOperations: [],
    availableOperations: [],
    selectedOperations: new Set(),
    showFilterConfig: false,
    filterLoading: false,
    collapsedGroups: new Set(),
    // Known-service credential helpers
    knownService: "",
    kubeconfigStatus: null,
    showSecret: false,
    // OAuth 2.0 (Gmail)
    oauthClientId: "",
    oauthClientSecret: "",
    oauthRefreshToken: "",
    oauthEmail: "",
    oauthConnected: false,
  };
}

createApp({
  setup() {
    const profiles = ref([]);
    const activeProfile = ref("");
    const originalProfileName = ref(""); // Track loaded name for rename detection
    const profileMetadata = ref({}); // Store API count and types for each profile
    const form = reactive({
      profileName: "",
      profileToken: "",
      apis: [],
    });
    const draft = reactive({
      baseUrl: "",
      name: "",
      type: "",
      specUrl: "",
      detectedOptions: [],
      status: "",
      detectedOnce: false,
      authType: "none",
      bearerToken: "",
      basicUser: "",
      basicPass: "",
      apiKeyHeader: "X-API-Key",
      apiKeyValue: "",
      knownService: "",
      kubeconfigStatus: null,
    });
    const status = reactive({ state: "idle", message: "" });
    const isBusy = ref(false);
    const addedToast = ref(false);
    const addFlow = reactive({
      open: false,
      step: "pick", // 'pick' | 'kubernetes' | 'gitlab' | 'jira' | 'slack' | 'gmail' | 'custom' | 'library'
      kubeParsed: null,
      kubeStatus: null,
      kubeTutorial: false,
      apiName: "",
      instanceUrl: "",
      email: "",
      token: "",
      customUrl: "",
      detecting: false,
      detectError: "",
      detectResults: [],
      busy: false,
      error: "",
      // Gmail OAuth fields
      gmailClientId: "",
      gmailClientSecret: "",
      gmailScopes: "https://www.googleapis.com/auth/gmail.modify",
      gmailTutorial: false,
      oauthRedirectUri: "",
      // Library import fields
      libraryItems: [],
      libraryLoading: false,
      libraryError: "",
      librarySearch: "",
      libraryCategory: "All",
    });
    const showToken = ref(false);
    const oauthRedirectHint = window.location.origin;

    // Export flow
    const exportFlow = reactive({
      open: false,
      busy: false,
      error: '',
      apis: {},   // keyed by api.id: { selected, includeAuth, includeFilter, name }
      encrypt: false,
      password: '',
      showPassword: false,
    });

    // Import flow
    const importFlow = reactive({
      open: false,
      step: 'pick',   // 'pick' | 'password' | 'merge'
      busy: false,
      error: '',
      file: null,
      targetProfile: '',
      newProfileName: '',
      password: '',
      showPassword: false,
      importApis: [],  // { name, spec_url, hasAuth, hasFilter, selected, importAuth, importFilter, conflicts, _raw }
    });
    const showNewProfileModal = ref(false);
    const newProfileName = ref("");
    const newProfileError = ref("");
    const selectedApiId = ref("");
    const profileTab = ref("overview");
    const expandedProfiles = ref({});
    const profileStats = ref(null);
    const profileMetrics = ref(null);
    const statsLoading = ref(false);
    let isLoadingProfile = false;

    // View-filter state (outside form.apis to avoid triggering auto-save)
    const filterViewState = reactive({});
    function getFilterView(api) {
      if (!filterViewState[api.id]) {
        filterViewState[api.id] = { searchQuery: "", activeMethodFilters: new Set() };
      }
      return filterViewState[api.id];
    }

    // MCP client connect section
    const connectPanel = ref("");
    const mcpUrl = computed(() => {
      if (!form.profileName) return "";
      const proto = window.location.protocol === "https:" ? "https:" : "http:";
      return `${proto}//${window.location.host}/profiles/${encodeURIComponent(form.profileName)}/mcp`;
    });
    const claudeDesktopSnippet = computed(() => {
      if (!form.profileName || !form.profileToken) return "";
      return JSON.stringify({
        mcpServers: {
          [`skyline-${form.profileName}`]: {
            url: mcpUrl.value,
            headers: { Authorization: `Bearer ${form.profileToken}` },
          },
        },
      }, null, 2);
    });
    const claudeCodeCmd = computed(() => {
      if (!form.profileName || !form.profileToken) return "";
      return `claude mcp add skyline-${form.profileName} --transport http ${mcpUrl.value} --header "Authorization: Bearer ${form.profileToken}"`;
    });
    const claudeCodeSettings = computed(() => {
      if (!form.profileName || !form.profileToken) return "";
      return JSON.stringify({
        mcpServers: {
          [`skyline-${form.profileName}`]: {
            url: mcpUrl.value,
            headers: { Authorization: `Bearer ${form.profileToken}` },
          },
        },
      }, null, 2);
    });
    const clineSnippet = computed(() => {
      if (!form.profileName || !form.profileToken) return "";
      return JSON.stringify({
        mcpServers: {
          [`skyline-${form.profileName}`]: {
            url: mcpUrl.value,
            headers: { Authorization: `Bearer ${form.profileToken}` },
          },
        },
      }, null, 2);
    });
    const codexSnippet = computed(() => {
      if (!form.profileName || !form.profileToken) return "";
      const key = `skyline-${form.profileName}`;
      return `[mcp_servers.${key}]\nurl = "${mcpUrl.value}"\nhttp_headers = { Authorization = "Bearer ${form.profileToken}" }`;
    });
    async function copySnippet(text) {
      try {
        await navigator.clipboard.writeText(text);
      } catch (e) {
        const el = document.createElement("textarea");
        el.value = text;
        document.body.appendChild(el);
        el.select();
        document.execCommand("copy");
        document.body.removeChild(el);
      }
    }

    async function refreshProfiles() {
      try {
        const data = await apiClient.listProfiles();
        profiles.value = data.profiles || [];

        // Load metadata for all profiles
        for (const name of profiles.value) {
          try {
            const profileData = await apiClient.loadProfile(name);
            const cfg = profileData.config || {};
            const apis = cfg.apis || [];
            const apiTypes = new Set();
            const knownServices = new Set();
            const apiList = [];
            for (const api of apis) {
              const t = inferType(api.spec_url || "");
              const svc = inferKnownService(api.base_url_override || "", api.spec_url || "", t);
              if (svc) knownServices.add(svc); else if (t) apiTypes.add(t);
              apiList.push({ name: api.name || "", specUrl: api.spec_url || "", type: t, knownService: svc });
            }
            profileMetadata.value[name] = {
              apiCount: apis.length,
              types: Array.from(apiTypes),
              knownServices: Array.from(knownServices),
              apis: apiList,
            };
          } catch (err) {
            // Ignore errors loading individual profiles (might be auth issues)
            console.warn(`Failed to load metadata for profile ${name}:`, err);
          }
        }
      } catch (err) {
        status.state = "error";
        status.message = err.message;
      }
    }

    function setStatus(state, message) {
      status.state = state;
      status.message = message;
    }

    function removeApi(id) {
      form.apis = form.apis.filter((api) => api.id !== id);
    }

    async function detectApi(api) {
      if (!api.baseUrl) {
        setStatus("error", "Base URL is required.");
        return;
      }
      try {
        isBusy.value = true;
        setStatus("idle", "");
        const token = api.authType === "bearer" ? api.bearerToken : null;
        const data = await apiClient.detect(api.baseUrl, token);
        const found = (data.detected || []).filter((d) => d.found);
        if (found.length === 0) {
          api.type = "";
          api.specUrl = "";
          api.status = data.online ? "Online, but no supported API detected." : "Endpoint not responding.";
          setStatus("error", api.status);
          return;
        }
        api.detectedOptions = found;
        selectDetectedOption(api, found[0]);
        api.knownService = inferKnownService(api.baseUrl, api.specUrl, api.type);
        if (!api.name) {
          const host = domainFromBaseURL(api.baseUrl);
          const label = serviceLabels[api.knownService] || typeLabels[api.type] || api.type;
          api.name = host ? `${host} - ${label}` : `${label}`;
        }
        api.detectedOnce = true;
        setStatus("ok", "Detection successful.");
      } catch (err) {
        setStatus("error", err.message);
      } finally {
        isBusy.value = false;
      }
    }

    async function detectDraft() {
      if (!draft.baseUrl) {
        setStatus("error", "Base URL is required.");
        return;
      }
      try {
        isBusy.value = true;
        setStatus("idle", "");
        const data = await apiClient.detect(draft.baseUrl);
        const found = (data.detected || []).filter((d) => d.found);
        if (found.length === 0) {
          draft.type = "";
          draft.specUrl = "";
          draft.status = data.online ? "Online, but no supported API detected." : "Endpoint not responding.";
          setStatus("error", draft.status);
          return;
        }
        draft.detectedOptions = found;
        selectDetectedOption(draft, found[0]);
        draft.knownService = inferKnownService(draft.baseUrl, draft.specUrl, draft.type);
        if (!draft.name) {
          const host = domainFromBaseURL(draft.baseUrl);
          const label = serviceLabels[draft.knownService] || typeLabels[draft.type] || draft.type;
          draft.name = host ? `${host} - ${label}` : `${label}`;
        }
        draft.detectedOnce = true;
        draft.status = `Detected ${serviceLabels[draft.knownService] || typeLabels[draft.type] || draft.type}`;
        setStatus("ok", "Detection successful.");
      } catch (err) {
        setStatus("error", err.message);
      } finally {
        isBusy.value = false;
      }
    }

    async function loadProfile(name) {
      try {
        isBusy.value = true;
        isLoadingProfile = true;
        // Load profile without authentication (for UI management)
        // Profile tokens are needed by MCP clients for authentication
        const data = await apiClient.loadProfile(name);
        form.profileName = data.name || name;
        form.profileToken = data.token || "";
        originalProfileName.value = name;
        activeProfile.value = name;
        const cfg = data.config || {};
        form.apis = (cfg.apis || []).map((api) => ({
          id: generateUUID(),
          name: api.name || "",
          baseUrl: api.base_url_override || "",
          specUrl: api.spec_url || "",
          type: inferType(api.spec_url || ""),
          status: "",
          detectedOptions: [],
          authType: api.auth?.type || "none",
          bearerToken: api.auth?.token || "",
          basicUser: api.auth?.username || "",
          basicPass: api.auth?.password || "",
          apiKeyHeader: api.auth?.header || "X-API-Key",
          apiKeyValue: api.auth?.value || "",
          oauthClientId: api.auth?.client_id || "",
          oauthClientSecret: api.auth?.client_secret || "",
          oauthRefreshToken: api.auth?.refresh_token || "",
          oauthEmail: "",
          oauthConnected: !!(api.auth?.refresh_token),
          detectedOnce: true,
          // Response truncation
          maxResponseBytes: api.max_response_bytes != null ? String(api.max_response_bytes) : "",
          // Load filter configuration
          filterMode: api.filter?.mode || "",
          filterOperations: api.filter?.operations || [],
          availableOperations: [],
          selectedOperations: new Set(
            (api.filter?.operations || [])
              .filter((op) => op.operation_id)
              .map((op) => op.operation_id)
          ),
          showFilterConfig: false,
          filterLoading: false,
          collapsedGroups: new Set(),
          knownService: inferKnownService(api.base_url_override || "", api.spec_url || "", inferType(api.spec_url || "")),
          kubeconfigStatus: null,
        }));

        // Extract and store profile metadata for sidebar display
        const apiTypes = new Set();
        const knownServices = new Set();
        for (const api of form.apis) {
          if (api.knownService) knownServices.add(api.knownService);
          else if (api.type) apiTypes.add(api.type);
        }
        profileMetadata.value[name] = {
          apiCount: form.apis.length,
          types: Array.from(apiTypes),
          knownServices: Array.from(knownServices),
          apis: form.apis.map(a => ({ name: a.name, specUrl: a.specUrl, type: a.type, knownService: a.knownService })),
        };

        await nextTick();
        isLoadingProfile = false;
        setStatus("ok", "Profile loaded.");
      } catch (err) {
        isLoadingProfile = false;
        if (err.message.includes("401") || err.message.includes("unauthorized")) {
          setStatus("error", `Authentication required. Run the server with "--auth-mode none" for UI access.`);
        } else {
          setStatus("error", `Failed to load profile: ${err.message}`);
        }
      } finally {
        isBusy.value = false;
      }
    }

    function inferType(specUrl) {
      const lower = specUrl.toLowerCase();
      if (lower.includes("swagger-v3.v3.json")) return "jira-rest";
      if (lower.includes("swagger")) return "swagger2";
      if (lower.includes("openapi")) return "openapi";
      if (lower.includes("graphql")) return "graphql";
      if (lower.includes("wsdl")) return "wsdl";
      if (lower.includes("$metadata") || lower.includes("odata")) return "odata";
      if (lower.includes("postman")) return "postman";
      if (lower.includes("openrpc") || lower.includes("jsonrpc")) return "openrpc";
      if (lower.includes("asyncapi")) return "asyncapi";
      if (lower.includes(".raml")) return "raml";
      if (lower.includes(".apib") || lower.includes("apiblueprint")) return "apiblueprint";
      if (lower.includes("insomnia")) return "insomnia";
      return "";
    }

    function domainFromBaseURL(baseUrl) {
      try {
        const url = new URL(baseUrl);
        return url.hostname || baseUrl;
      } catch {
        const trimmed = baseUrl.replace(/^https?:\/\//i, "").split("/")[0];
        return trimmed || baseUrl;
      }
    }

    async function detectOnBlur(api) {
      if (!api.baseUrl || api.detectedOnce) {
        return;
      }
      await detectApi(api);
    }

    async function testApi(api) {
      if (!api.specUrl) {
        setStatus("error", "Spec URL is required to test.");
        return;
      }
      try {
        isBusy.value = true;
        const result = await apiClient.testSpec(api.specUrl);
        if (result.online) {
          setStatus("ok", `Spec reachable (${result.status}).`);
        } else {
          setStatus("error", `Spec not reachable (${result.status}).`);
        }
      } catch (err) {
        setStatus("error", err.message);
      } finally {
        isBusy.value = false;
      }
    }

    async function detectDraftOnBlur() {
      if (!draft.baseUrl || draft.detectedOnce) {
        return;
      }
      await detectDraft();
    }

    function selectDetectedOption(api, option) {
      if (!option) return;
      api.type = option.type;
      api.specUrl = option.spec_url;
      api.status = `Detected ${typeLabels[option.type] || option.type}`;
      api.detectedOnce = true;
    }

    function addDraftToList() {
      if (!draft.detectedOnce || !draft.specUrl) {
        setStatus("error", "Detect the API before adding it.");
        return;
      }
      const existing = form.apis.find(
        (api) => api.specUrl.trim() === draft.specUrl.trim() || api.baseUrl.trim() === draft.baseUrl.trim()
      );
      if (existing) {
        setStatus("error", "API already exists in the list.");
        return;
      }
      const api = blankApi();
      api.baseUrl = draft.baseUrl;
      api.specUrl = draft.specUrl;
      api.type = draft.type;
      api.name = draft.name;
      api.detectedOptions = draft.detectedOptions;
      api.status = draft.status;
      api.authType = draft.authType;
      api.bearerToken = draft.bearerToken;
      api.basicUser = draft.basicUser;
      api.basicPass = draft.basicPass;
      api.apiKeyHeader = draft.apiKeyHeader;
      api.apiKeyValue = draft.apiKeyValue;
      api.detectedOnce = true;
      form.apis.push(api);
      resetDraft();
      addedToast.value = true;
      setTimeout(() => {
        addedToast.value = false;
      }, 1500);
    }

    function resetDraft() {
      draft.baseUrl = "";
      draft.name = "";
      draft.type = "";
      draft.specUrl = "";
      draft.detectedOptions = [];
      draft.status = "";
      draft.detectedOnce = false;
      draft.authType = "none";
      draft.bearerToken = "";
      draft.basicUser = "";
      draft.basicPass = "";
      draft.apiKeyHeader = "X-API-Key";
      draft.apiKeyValue = "";
    }

    // ── Computed ────────────────────────────────────────────────────────────────

    const selectedApi = computed(() => form.apis.find(a => a.id === selectedApiId.value) || null);

    // ── Profile tree navigation ─────────────────────────────────────────────────

    function toggleProfileExpand(name) {
      expandedProfiles.value = { ...expandedProfiles.value, [name]: !expandedProfiles.value[name] };
    }

    async function loadProfileStats(name) {
      statsLoading.value = true;
      profileStats.value = null;
      profileMetrics.value = null;
      try {
        const since = new Date(Date.now() - 24 * 3600 * 1000).toISOString();
        const res = await fetch(`/admin/stats?profile=${encodeURIComponent(name)}&since=${encodeURIComponent(since)}`);
        if (res.ok) {
          const data = await res.json();
          profileStats.value = data.audit_stats || {};
          profileMetrics.value = data.metrics_snapshot || {};
        } else {
          profileStats.value = {};
          profileMetrics.value = {};
        }
      } catch {
        profileStats.value = {};
        profileMetrics.value = {};
      } finally {
        statsLoading.value = false;
      }
    }

    async function selectProfile(name) {
      selectedApiId.value = "";
      profileTab.value = "overview";
      expandedProfiles.value = { ...expandedProfiles.value, [name]: true };
      await loadProfile(name);
      loadProfileStats(name);
    }

    function selectApi(apiId) {
      selectedApiId.value = apiId;
    }

    function backToProfile() {
      selectedApiId.value = "";
    }

    async function selectApiBySpecUrl(profileName, specUrl) {
      if (activeProfile.value !== profileName) {
        await selectProfile(profileName);
      }
      const api = form.apis.find(a => a.specUrl === specUrl);
      if (api) selectApi(api.id);
    }

    // ── Auto-save on API config change ──────────────────────────────────────────

    let saveTimer = null;
    watch(() => form.apis, () => {
      if (!activeProfile.value || isLoadingProfile) return;
      if (saveTimer) clearTimeout(saveTimer);
      saveTimer = setTimeout(() => saveProfile(true), 1500);
    }, { deep: true });

    async function saveProfile(silent = false) {
      if (!form.profileName) {
        setStatus("error", "Profile name required.");
        return;
      }

      // Auto-generate token if not present
      if (!form.profileToken) {
        const array = new Uint8Array(32);
        crypto.getRandomValues(array);
        form.profileToken = btoa(String.fromCharCode.apply(null, array));
      }
      const apis = form.apis
        .filter((api) => {
          const hasName = api.name?.trim();
          const hasSpecUrl = api.specUrl?.trim();
          console.log('Filtering API:', { name: api.name, specUrl: api.specUrl, hasName, hasSpecUrl });
          return hasName && hasSpecUrl;
        })
        .map((api) => {
          const entry = {
            name: api.name.trim(),
            spec_url: api.specUrl.trim(),
            base_url_override: api.baseUrl?.trim() || undefined,
          };
          if (api.authType && api.authType !== "none") {
            entry.auth = { type: api.authType };
            if (api.authType === "bearer") entry.auth.token = api.bearerToken;
            if (api.authType === "basic") {
              entry.auth.username = api.basicUser;
              entry.auth.password = api.basicPass;
            }
            if (api.authType === "api-key") {
              entry.auth.header = api.apiKeyHeader;
              entry.auth.value = api.apiKeyValue;
            }
            if (api.authType === "oauth2") {
              entry.auth.client_id = api.oauthClientId;
              entry.auth.client_secret = api.oauthClientSecret;
              entry.auth.refresh_token = api.oauthRefreshToken;
            }
          }
          // Include per-API max response bytes if set
          const mrb = parseInt(api.maxResponseBytes, 10);
          if (!isNaN(mrb) && mrb >= 0) {
            entry.max_response_bytes = mrb;
          }
          // Include filter configuration if set
          if (api.filterMode && api.filterOperations.length > 0) {
            entry.filter = {
              mode: api.filterMode,
              operations: api.filterOperations,
            };
          }
          return entry;
        });

      try {
        isBusy.value = true;
        const isRename = originalProfileName.value && form.profileName !== originalProfileName.value;
        await apiClient.saveProfile(form.profileName, form.profileToken, { apis });

        // If name changed, delete the old profile
        if (isRename) {
          await apiClient.deleteProfile(originalProfileName.value);
          delete profileMetadata.value[originalProfileName.value];
        }

        await refreshProfiles();
        originalProfileName.value = form.profileName;
        activeProfile.value = form.profileName;

        // Update profile metadata after saving
        const apiTypes = new Set();
        const knownServices = new Set();
        for (const api of form.apis) {
          if (api.knownService) knownServices.add(api.knownService);
          else if (api.type) apiTypes.add(api.type);
        }
        profileMetadata.value[form.profileName] = {
          apiCount: form.apis.length,
          types: Array.from(apiTypes),
          knownServices: Array.from(knownServices),
          apis: form.apis.map(a => ({ name: a.name, specUrl: a.specUrl, type: a.type, knownService: a.knownService })),
        };

        if (!silent) setStatus("ok", isRename ? "Profile renamed and saved." : "Profile saved.");
      } catch (err) {
        setStatus("error", err.message);
      } finally {
        isBusy.value = false;
      }
    }

    function openNewProfileModal() {
      newProfileName.value = "";
      newProfileError.value = "";
      showNewProfileModal.value = true;
    }

    function closeNewProfileModal() {
      showNewProfileModal.value = false;
      newProfileName.value = "";
      newProfileError.value = "";
    }

    async function createNewProfile() {
      const name = newProfileName.value.trim();
      if (!name) {
        newProfileError.value = "Profile name is required.";
        return;
      }
      if (profiles.value.includes(name)) {
        newProfileError.value = "A profile with this name already exists.";
        return;
      }
      try {
        isBusy.value = true;
        const array = new Uint8Array(32);
        crypto.getRandomValues(array);
        const token = btoa(String.fromCharCode.apply(null, array));
        await apiClient.saveProfile(name, token, { apis: [] });
        await refreshProfiles();
        closeNewProfileModal();
        await loadProfile(name);
        setStatus("ok", "Profile created. Add APIs below.");
      } catch (err) {
        newProfileError.value = err.message;
      } finally {
        isBusy.value = false;
      }
    }

    async function deleteProfile() {
      if (!form.profileName) return;
      if (!confirm(`Delete profile "${form.profileName}"?`)) return;
      const deletedProfileName = form.profileName;
      try {
        isBusy.value = true;
        // Delete profile without authentication (for UI management)
        await apiClient.deleteProfile(form.profileName);
        await refreshProfiles();

        // Remove metadata for deleted profile
        delete profileMetadata.value[deletedProfileName];

        form.profileName = "";
        form.profileToken = "";
        form.apis = [];
        originalProfileName.value = "";
        activeProfile.value = "";
        setStatus("ok", "Profile deleted.");
      } catch (err) {
        setStatus("error", err.message);
      } finally {
        isBusy.value = false;
      }
    }

    async function toggleFilterConfig(api) {
      api.showFilterConfig = !api.showFilterConfig;
      // Always fetch operations when opening (auto-load + refresh)
      if (api.showFilterConfig) {
        await fetchOperations(api);
      }
    }

    async function fetchOperations(api) {
      if (!api.specUrl && !api.name) {
        setStatus("error", "Spec URL or API name is required to fetch operations.");
        return;
      }
      try {
        api.filterLoading = true;
        const result = await apiClient.fetchOperations(api.specUrl, api.type, api.name);
        if (result.error) {
          setStatus("error", result.error);
          return;
        }
        api.availableOperations = result.operations || [];

        // Pre-select operations based on current state
        if (api.filterMode) {
          // Mode already selected - apply mode logic
          onFilterModeChange(api);
        } else if (api.filterOperations.length > 0) {
          // No mode yet, but has saved filter - restore selections
          api.filterOperations.forEach((filter) => {
            if (filter.operation_id) {
              api.availableOperations.forEach((op) => {
                if (op.id === filter.operation_id) {
                  api.selectedOperations.add(op.id);
                }
              });
            }
          });
        } else {
          // No filter configured yet - show ALL operations as checked (current state: all allowed)
          api.availableOperations.forEach((op) => {
            api.selectedOperations.add(op.id);
          });
        }

        // Start with all groups collapsed
        api.collapsedGroups = new Set(
          groupOperations(api.availableOperations).map(g => g.name)
        );

        // Reset view filters
        const fv = getFilterView(api);
        fv.searchQuery = "";
        fv.activeMethodFilters = new Set();

        const modeHint = api.filterMode ? "" : " All operations currently allowed. Choose filter mode to restrict.";
        setStatus("ok", `Loaded ${api.availableOperations.length} operations.${modeHint}`);
      } catch (err) {
        setStatus("error", err.message);
      } finally {
        api.filterLoading = false;
      }
    }

    function toggleOperationSelection(api, operationId) {
      if (api.selectedOperations.has(operationId)) {
        api.selectedOperations.delete(operationId);
      } else {
        api.selectedOperations.add(operationId);
      }

      // Auto-apply filter when selection changes
      if (api.filterMode && api.selectedOperations.size > 0) {
        applyFilterAuto(api);
      }
    }

    function applyFilterAuto(api) {
      // Convert selected operations to filter patterns
      api.filterOperations = Array.from(api.selectedOperations).map((opId) => ({
        operation_id: opId,
      }));

      const count = api.selectedOperations.size;
      const total = api.availableOperations.length;
      const willExpose = api.filterMode === "allowlist" ? count : total - count;

      setStatus("ok", `Filter updated: ${willExpose} of ${total} operations will be exposed.`);
    }

    function onFilterModeChange(api) {
      // Mode acts as "select all / deselect all" toggle
      if (api.availableOperations.length > 0) {
        api.selectedOperations.clear();

        if (api.filterMode === "allowlist") {
          // Allowlist: Check ALL operations (allow everything, then uncheck unwanted)
          api.availableOperations.forEach((op) => {
            api.selectedOperations.add(op.id);
          });
          setStatus("ok", `Allowlist mode: All ${api.availableOperations.length} operations selected. Uncheck what you don't want to allow.`);
        } else if (api.filterMode === "blocklist") {
          // Blocklist: Check NONE (block nothing, then check what you want to block)
          setStatus("ok", `Blocklist mode: No operations selected. Check what you want to block.`);
        }
      }

      // Auto-apply filter
      if (api.filterMode && api.availableOperations.length > 0) {
        applyFilterAuto(api);
      }
    }

    function clearFilter(api) {
      api.filterMode = "";
      api.filterOperations = [];
      api.selectedOperations.clear();
      api.collapsedGroups = new Set();
      setStatus("ok", "Filter cleared.");
    }

    // Group operations by resource path prefix for easier navigation.
    // e.g. /rest/api/2/issue/{id} and /rest/api/2/issue → group "issue"
    function groupOperations(operations) {
      const groups = new Map();
      for (const op of operations) {
        const key = extractResourceGroup(op.path, op.id);
        if (!groups.has(key)) {
          groups.set(key, []);
        }
        groups.get(key).push(op);
      }
      // Sort groups alphabetically, return as array of { name, operations }
      return Array.from(groups.entries())
        .sort((a, b) => a[0].localeCompare(b[0]))
        .map(([name, ops]) => ({ name, operations: ops }));
    }

    function extractResourceGroup(path, operationId) {
      // Dot-notation operationIds (Google Discovery: "users.messages.list")
      // encode resource hierarchy better than paths for these APIs.
      if (operationId && operationId.includes(".")) {
        const idParts = operationId.split(".");
        // 3+: "users.messages.list" → "Messages"
        if (idParts.length >= 3) {
          const r = idParts[idParts.length - 2];
          return r.charAt(0).toUpperCase() + r.slice(1);
        }
        // 2: "conversations.list" → "Conversations"
        if (idParts.length === 2) {
          const r = idParts[0];
          return r.charAt(0).toUpperCase() + r.slice(1);
        }
      }
      if (!path || path === "/") {
        // Fall back to operation ID prefix (e.g., "getIssue" → "issue")
        const match = operationId?.match(/^(?:get|list|create|update|delete|search|find|add|remove|set|bulk)(.+?)(?:By.*)?$/i);
        if (match) return match[1].replace(/([a-z])([A-Z])/g, "$1 $2").trim();
        return "Other";
      }
      // Strip common API version prefixes
      let p = path.replace(/^\/(?:rest\/)?(?:api\/)?(?:v?\d+\/)?/, "/");
      // Split and find the first meaningful segment
      const parts = p.split("/").filter(Boolean);
      if (parts.length === 0) return "Root";
      // Skip path parameters ({…}) and generic prefixes to find the resource name
      let resource = parts.find(seg => !seg.startsWith("{") && !/^(?:api|rest|v\d+)$/i.test(seg));
      if (!resource) return "Other";
      return resource.charAt(0).toUpperCase() + resource.slice(1);
    }

    function toggleGroup(api, groupName) {
      if (!api.collapsedGroups) api.collapsedGroups = new Set();
      if (api.collapsedGroups.has(groupName)) {
        api.collapsedGroups.delete(groupName);
      } else {
        api.collapsedGroups.add(groupName);
      }
    }

    function toggleGroupSelection(api, groupOps) {
      const allSelected = groupOps.every(op => api.selectedOperations.has(op.id));
      if (allSelected) {
        groupOps.forEach(op => api.selectedOperations.delete(op.id));
      } else {
        groupOps.forEach(op => api.selectedOperations.add(op.id));
      }
      if (api.filterMode && api.selectedOperations.size > 0) {
        applyFilterAuto(api);
      }
    }

    // ── View-filter functions (method pills, search, bulk actions) ─────────────

    function allMethodsForApi(api) {
      const methods = new Set();
      for (const op of api.availableOperations) {
        methods.add(op.method.toUpperCase());
      }
      const order = ["GET", "POST", "PUT", "PATCH", "DELETE"];
      return Array.from(methods).sort((a, b) => {
        const ia = order.indexOf(a), ib = order.indexOf(b);
        if (ia !== -1 && ib !== -1) return ia - ib;
        if (ia !== -1) return -1;
        if (ib !== -1) return 1;
        return a.localeCompare(b);
      });
    }

    function isMethodActive(api, method) {
      const fv = getFilterView(api);
      if (fv.activeMethodFilters.size === 0) return true;
      return fv.activeMethodFilters.has(method);
    }

    function toggleMethodFilter(api, method) {
      const fv = getFilterView(api);
      const allMethods = allMethodsForApi(api);
      if (fv.activeMethodFilters.size === 0) {
        // All currently active — deactivate clicked one
        allMethods.forEach(m => { if (m !== method) fv.activeMethodFilters.add(m); });
      } else if (fv.activeMethodFilters.has(method)) {
        if (fv.activeMethodFilters.size > 1) {
          fv.activeMethodFilters.delete(method);
        }
      } else {
        fv.activeMethodFilters.add(method);
        if (fv.activeMethodFilters.size === allMethods.length) {
          fv.activeMethodFilters.clear();
        }
      }
    }

    function filteredOperations(api) {
      const fv = getFilterView(api);
      let ops = api.availableOperations;
      if (fv.activeMethodFilters.size > 0) {
        ops = ops.filter(op => fv.activeMethodFilters.has(op.method.toUpperCase()));
      }
      const q = (fv.searchQuery || "").trim().toLowerCase();
      if (q) {
        ops = ops.filter(op =>
          (op.id && op.id.toLowerCase().includes(q)) ||
          (op.path && op.path.toLowerCase().includes(q)) ||
          (op.summary && op.summary.toLowerCase().includes(q))
        );
      }
      return ops;
    }

    function filteredGroups(api) {
      return groupOperations(filteredOperations(api));
    }

    function selectVisible(api) {
      filteredOperations(api).forEach(op => api.selectedOperations.add(op.id));
      if (api.filterMode) applyFilterAuto(api);
    }

    function deselectVisible(api) {
      filteredOperations(api).forEach(op => api.selectedOperations.delete(op.id));
      if (api.filterMode) applyFilterAuto(api);
    }

    function clearSearch(api) {
      getFilterView(api).searchQuery = "";
    }

    function toggleTokenVisibility() {
      showToken.value = !showToken.value;
    }

    // ── Known-service credential helpers ───────────────────────────────────────

    function slackTokenType(token) {
      if (!token) return null;
      if (token.startsWith("xoxb-")) return "bot";
      if (token.startsWith("xoxp-")) return "user";
      return null;
    }

    function applySimpleToken(target, token) {
      target.authType = "bearer";
      target.bearerToken = token;
    }

    /** Parse a kubeconfig YAML and extract the server URL + bearer token for the active context. */
    function parseKubeconfig(text) {
      const kc = jsyaml.load(text);
      const ctxName = kc["current-context"];
      if (!ctxName) throw new Error("No current-context in kubeconfig");
      const ctx = (kc.contexts || []).find((c) => c.name === ctxName)?.context;
      if (!ctx) throw new Error(`Context "${ctxName}" not found`);
      const user = (kc.users || []).find((u) => u.name === ctx.user)?.user;
      if (!user) throw new Error(`User "${ctx.user}" not found`);
      const cluster = (kc.clusters || []).find((c) => c.name === ctx.cluster)?.cluster;
      return {
        serverUrl: cluster?.server || "",
        token: user.token || null,
        hasClientCert: !!(user["client-certificate-data"] && user["client-key-data"]),
      };
    }

    async function handleKubeconfigUpload(target, event) {
      const file = event.target.files[0];
      event.target.value = ""; // clear so the same file can be re-selected
      if (!file) return;
      try {
        const text = await file.text();
        const parsed = parseKubeconfig(text);
        if (parsed.token) {
          target.authType = "bearer";
          target.bearerToken = parsed.token;
          if (parsed.serverUrl && !target.baseUrl) target.baseUrl = parsed.serverUrl;
          target.kubeconfigStatus = { type: "success", message: "Token extracted from kubeconfig" };
        } else if (parsed.hasClientCert) {
          target.kubeconfigStatus = { type: "warn", message: "Client certificate auth found — use a service account token instead" };
        } else {
          target.kubeconfigStatus = { type: "error", message: "No token found — generate a service account token for this cluster" };
        }
      } catch (err) {
        target.kubeconfigStatus = { type: "error", message: "Parse failed: " + err.message };
      }
    }

    // ── Add API flow ────────────────────────────────────────────────────────────

    function openAddFlow() {
      Object.assign(addFlow, {
        open: true, step: "pick", kubeParsed: null, kubeStatus: null, kubeTutorial: false,
        apiName: "", instanceUrl: "", email: "", token: "",
        customUrl: "", detecting: false, detectError: "", detectResults: [], busy: false, error: "",
        libraryItems: [], libraryLoading: false, libraryError: "", librarySearch: "", libraryCategory: "All",
      });
    }

    function closeAddFlow() {
      addFlow.open = false;
    }

    function pickService(svc) {
      addFlow.step = svc;
      addFlow.error = "";
      if (svc === "gitlab" && !addFlow.instanceUrl) addFlow.instanceUrl = "https://gitlab.com";
      if (svc === "library") loadLibrary();
    }

    async function handleAddFlowKubeUpload(event) {
      const file = event.target.files[0];
      event.target.value = "";
      if (!file) return;
      try {
        const text = await file.text();
        const parsed = parseKubeconfig(text);
        addFlow.kubeParsed = parsed;
        if (parsed.token) {
          addFlow.kubeStatus = { type: "success", message: `Token extracted · Server: ${parsed.serverUrl}` };
          if (!addFlow.apiName) addFlow.apiName = "Kubernetes";
        } else if (parsed.hasClientCert) {
          addFlow.kubeStatus = { type: "warn", message: "Client certificate auth found — use a service account token instead" };
        } else {
          addFlow.kubeStatus = { type: "error", message: "No token found in kubeconfig" };
        }
      } catch (err) {
        addFlow.kubeStatus = { type: "error", message: "Parse failed: " + err.message };
        addFlow.kubeParsed = null;
      }
    }

    function addApiToProfile(api) {
      form.apis.push(api);
      closeAddFlow();
      addedToast.value = true;
      setTimeout(() => { addedToast.value = false; }, 1500);
    }

    async function addKubernetes() {
      const url = (addFlow.instanceUrl.trim() || "https://localhost:6443").replace(/\/$/, "");
      if (!addFlow.token.trim()) { addFlow.error = "Service account token is required."; return; }
      addFlow.busy = true;
      addFlow.error = "";
      try {
        const data = await apiClient.detect(url, addFlow.token.trim());
        const kubeProbes = (data.detected || []).filter(
          (p) => p.spec_url && (p.spec_url.endsWith("/openapi/v2") || p.spec_url.endsWith("/openapi/v3"))
        );
        // Any HTTP response means the cluster is reachable — distinguish by status
        const ok       = kubeProbes.find((p) => p.found && p.status >= 200 && p.status < 300);
        const reachable = kubeProbes.find((p) => p.status > 0); // got any HTTP response
        const noConnect = !reachable && !data.online;
        if (!ok) {
          if (noConnect) {
            addFlow.error = "Could not connect to cluster — check the URL.";
          } else {
            addFlow.error = "Token rejected by the cluster — check your service account token.";
          }
          return;
        }
        const api = blankApi();
        api.name = addFlow.apiName.trim() || "Kubernetes";
        api.baseUrl = url;
        api.specUrl = ok.spec_url;
        api.type = ok.type;
        api.knownService = "kubernetes";
        api.authType = "bearer";
        api.bearerToken = addFlow.token.trim();
        api.detectedOnce = true;
        addApiToProfile(api);
      } catch (err) {
        addFlow.error = "Verification failed: " + err.message;
      } finally {
        addFlow.busy = false;
      }
    }

    async function addGitLab() {
      const instanceUrl = addFlow.instanceUrl.trim().replace(/\/$/, "");
      if (!instanceUrl) { addFlow.error = "Instance URL is required."; return; }
      if (!addFlow.token.trim()) { addFlow.error = "Personal Access Token is required."; return; }
      addFlow.busy = true;
      addFlow.error = "";
      try {
        const res = await fetch("/verify", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ service: "gitlab", base_url: instanceUrl, token: addFlow.token.trim() }),
        });
        const data = await res.json();
        if (!data.ok) {
          addFlow.error = data.error === "auth_error"
            ? "Token rejected by GitLab — check your Personal Access Token."
            : `GitLab verification failed: ${data.error || "could not connect"}`;
          return;
        }
      } catch (err) {
        addFlow.error = "Could not reach GitLab: " + err.message;
        return;
      } finally {
        addFlow.busy = false;
      }
      const api = blankApi();
      api.name = addFlow.apiName.trim() || "GitLab";
      api.baseUrl = instanceUrl;
      api.specUrl = instanceUrl + "/api/openapi.json";
      api.type = "openapi";
      api.knownService = "gitlab";
      api.authType = "bearer";
      api.bearerToken = addFlow.token.trim();
      api.detectedOnce = true;
      addApiToProfile(api);
    }

    async function addJira() {
      const instanceUrl = addFlow.instanceUrl.trim().replace(/\/$/, "");
      if (!instanceUrl) { addFlow.error = "Instance URL is required."; return; }
      if (!addFlow.token.trim()) { addFlow.error = "API token is required."; return; }
      addFlow.busy = true;
      addFlow.error = "";
      try {
        const res = await fetch("/verify", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            service: "jira",
            base_url: instanceUrl,
            email: addFlow.email.trim(),
            token: addFlow.token.trim(),
          }),
        });
        const data = await res.json();
        if (!data.ok) {
          addFlow.error = data.error === "auth_error"
            ? "Credentials rejected by Jira — check your token."
            : `Jira verification failed: ${data.error || "could not connect"}`;
          return;
        }
      } catch (err) {
        addFlow.error = "Could not reach Jira: " + err.message;
        return;
      } finally {
        addFlow.busy = false;
      }
      const isCloud = instanceUrl.includes(".atlassian.net");
      const api = blankApi();
      api.name = addFlow.apiName.trim() || "Jira";
      api.baseUrl = instanceUrl;
      api.specUrl = isCloud
        ? "https://developer.atlassian.com/cloud/jira/platform/swagger-v3.v3.json"
        : instanceUrl + "/rest/api/3/openapi.json";
      api.type = isCloud ? "jira-rest" : "openapi";
      api.knownService = "jira";
      if (isCloud && addFlow.email.trim()) {
        api.authType = "basic";
        api.basicUser = addFlow.email.trim();
        api.basicPass = addFlow.token.trim();
      } else {
        api.authType = "bearer";
        api.bearerToken = addFlow.token.trim();
      }
      api.detectedOnce = true;
      addApiToProfile(api);
    }

    async function addSlack() {
      if (!addFlow.token.trim()) { addFlow.error = "Token is required."; return; }
      addFlow.busy = true;
      addFlow.error = "";
      try {
        const res = await fetch("/verify", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ service: "slack", token: addFlow.token.trim() }),
        });
        const data = await res.json();
        if (!data.ok) {
          const msg = data.error;
          addFlow.error = (msg === "invalid_auth" || msg === "not_authed" || msg === "token_revoked")
            ? "Token rejected by Slack — check your token."
            : `Slack verification failed: ${msg || "unknown error"}`;
          return;
        }
      } catch (err) {
        addFlow.error = "Could not reach Slack API: " + err.message;
        return;
      } finally {
        addFlow.busy = false;
      }
      const tokenType = slackTokenType(addFlow.token);
      const api = blankApi();
      api.name = addFlow.apiName.trim() || (tokenType === "bot" ? "Slack Bot" : tokenType === "user" ? "Slack User" : "Slack");
      api.baseUrl = "https://slack.com/api";
      api.specUrl = "https://api.slack.com/specs/openapi/api_v2.json";
      api.type = "openapi";
      api.knownService = "slack";
      api.authType = "bearer";
      api.bearerToken = addFlow.token.trim();
      api.detectedOnce = true;
      addApiToProfile(api);
    }

    async function addGmail() {
      if (!addFlow.gmailClientId.trim()) { addFlow.error = "Client ID is required."; return; }
      if (!addFlow.gmailClientSecret.trim()) { addFlow.error = "Client Secret is required."; return; }
      addFlow.busy = true;
      addFlow.error = "";
      try {
        // Step 1: Get the OAuth URL from the backend
        const startRes = await fetch("/oauth/start", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            client_id: addFlow.gmailClientId.trim(),
            scopes: addFlow.gmailScopes,
          }),
        });
        const startData = await startRes.json();
        if (!startData.auth_url) { addFlow.error = "Failed to generate OAuth URL."; return; }
        addFlow.oauthRedirectUri = startData.redirect_uri;

        // Step 2: Open the OAuth popup
        const popup = window.open(startData.auth_url, "skyline-oauth",
          "width=600,height=700,left=200,top=100");
        if (!popup) {
          addFlow.error = "Popup blocked — please allow popups for this site and try again.";
          return;
        }

        // Step 3: Listen for the callback message
        const code = await new Promise((resolve, reject) => {
          const timeout = setTimeout(() => {
            window.removeEventListener("message", handler);
            reject(new Error("OAuth timed out — close the popup and try again."));
          }, 300000);
          function handler(event) {
            if (event.origin !== window.location.origin) return;
            if (event.data?.type !== "skyline-oauth-callback") return;
            clearTimeout(timeout);
            window.removeEventListener("message", handler);
            if (event.data.success && event.data.code) {
              resolve(event.data.code);
            } else {
              reject(new Error(event.data.error || "OAuth authorization failed."));
            }
          }
          window.addEventListener("message", handler);
        });

        // Step 4: Exchange the code for tokens
        const exchangeRes = await fetch("/oauth/exchange", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            code: code,
            client_id: addFlow.gmailClientId.trim(),
            client_secret: addFlow.gmailClientSecret.trim(),
            redirect_uri: addFlow.oauthRedirectUri,
          }),
        });
        const exchangeData = await exchangeRes.json();
        if (!exchangeData.ok) {
          addFlow.error = `Token exchange failed: ${exchangeData.error}`;
          return;
        }

        // Step 5: Verify the connection
        const verifyRes = await fetch("/verify", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ service: "gmail", access_token: exchangeData.access_token }),
        });
        const verifyData = await verifyRes.json();
        if (!verifyData.ok) {
          addFlow.error = `Gmail verification failed: ${verifyData.error}`;
          return;
        }

        // Step 6: Create the API entry
        const api = blankApi();
        api.name = addFlow.apiName.trim() || "Gmail";
        api.baseUrl = "https://gmail.googleapis.com";
        api.specUrl = "https://gmail.googleapis.com/$discovery/rest?version=v1";
        api.type = "google-discovery";
        api.knownService = "gmail";
        api.authType = "oauth2";
        api.oauthClientId = addFlow.gmailClientId.trim();
        api.oauthClientSecret = addFlow.gmailClientSecret.trim();
        api.oauthRefreshToken = exchangeData.refresh_token;
        api.oauthEmail = verifyData.email || "";
        api.oauthConnected = true;
        api.detectedOnce = true;
        addApiToProfile(api);
      } catch (err) {
        addFlow.error = err.message;
      } finally {
        addFlow.busy = false;
      }
    }

    async function runAddFlowDetect() {
      if (!addFlow.customUrl.trim()) { addFlow.error = "URL is required."; return; }
      try {
        addFlow.detecting = true;
        addFlow.detectError = "";
        addFlow.detectResults = [];
        addFlow.error = "";
        const data = await apiClient.detect(addFlow.customUrl.trim());
        const found = (data.detected || []).filter((d) => d.found);
        addFlow.detectResults = found;
        if (found.length === 0) {
          addFlow.detectError = data.online ? "Online, but no supported API detected." : "Endpoint not responding.";
        }
      } catch (err) {
        addFlow.detectError = err.message;
      } finally {
        addFlow.detecting = false;
      }
    }

    function addFromDetectResult(probe) {
      const baseUrl = addFlow.customUrl.trim().replace(/\/$/, "");
      const knownSvc = inferKnownService(baseUrl, probe.spec_url, probe.type);
      const host = domainFromBaseURL(baseUrl);
      const label = serviceLabels[knownSvc] || typeLabels[probe.type] || probe.type;
      const api = blankApi();
      api.name = host ? `${host} - ${label}` : label;
      api.baseUrl = baseUrl;
      api.specUrl = probe.spec_url;
      api.type = probe.type;
      api.knownService = knownSvc;
      api.detectedOnce = true;
      addApiToProfile(api);
    }

    // ── Library import ──────────────────────────────────────────────────────
    // Slim library endpoint — compact JSON with short keys (277 KB gzipped)
    // Keys: id, t=title, d=description, c=category, at=authType, su=specUrl, st=specType, bu=baseUrl, w=website
    const LIBRARY_URL = "https://raw.githubusercontent.com/emadomedher/skyline-api-library/main/profiles-slim.json";

    async function loadLibrary() {
      if (addFlow.libraryItems.length > 0) return; // already loaded
      addFlow.libraryLoading = true;
      addFlow.libraryError = "";
      try {
        const res = await fetch(LIBRARY_URL);
        if (!res.ok) throw new Error(`Failed to load library (${res.status})`);
        const data = await res.json();
        // Expand short keys to readable names for the UI
        addFlow.libraryItems = (data.profiles || []).map((p) => ({
          id: p.id,
          title: p.t,
          subtitle: p.d || "",
          category: p.c,
          authType: p.at,
          specUrl: p.su || "",
          specType: p.st || "",
          baseUrl: p.bu || "",
          website: p.w || "",
        }));
      } catch (err) {
        addFlow.libraryError = err.message;
      } finally {
        addFlow.libraryLoading = false;
      }
    }

    const libraryCategories = computed(() => {
      const cats = new Set(addFlow.libraryItems.map((p) => p.category));
      return ["All", ...Array.from(cats).sort()];
    });

    const filteredLibraryItems = computed(() => {
      let items = addFlow.libraryItems;
      if (addFlow.libraryCategory !== "All") {
        items = items.filter((p) => p.category === addFlow.libraryCategory);
      }
      const q = addFlow.librarySearch.toLowerCase().trim();
      if (q) {
        items = items.filter(
          (p) =>
            p.title.toLowerCase().includes(q) ||
            p.subtitle.toLowerCase().includes(q)
        );
      }
      return items;
    });

    function addFromLibrary(item) {
      // All fields are already in the slim payload — no second fetch needed
      const api = blankApi();
      api.name = item.title;
      api.baseUrl = item.baseUrl;
      api.specUrl = item.specUrl;
      api.type = item.specType;
      api.knownService = inferKnownService(api.baseUrl, api.specUrl, api.type);
      if (item.authType === "bearer") api.authType = "bearer";
      else if (item.authType === "basic") api.authType = "basic";
      else if (item.authType === "oauth2") api.authType = "oauth2";
      else if (item.authType === "api-key") api.authType = "api-key";
      api.detectedOnce = true;
      addApiToProfile(api);
    }

    onMounted(refreshProfiles);

    // ── Export ──────────────────────────────────────────────────────────────
    function openExportFlow() {
      if (!activeProfile.value || !form.apis.length) return;
      exportFlow.apis = {};
      for (const api of form.apis) {
        exportFlow.apis[api.id] = {
          selected: true,
          includeAuth: api.authType && api.authType !== 'none',
          includeFilter: !!(api.filterMode),
          name: api.name,
        };
      }
      exportFlow.encrypt = false;
      exportFlow.password = '';
      exportFlow.error = '';
      exportFlow.open = true;
    }

    function closeExportFlow() {
      exportFlow.open = false;
      exportFlow.password = '';
    }

    async function runExport() {
      const selected = form.apis.filter(api => exportFlow.apis[api.id]?.selected);
      if (selected.length === 0) { exportFlow.error = 'Select at least one API.'; return; }
      if (exportFlow.encrypt && !exportFlow.password) { exportFlow.error = 'Password is required for encryption.'; return; }
      exportFlow.busy = true;
      exportFlow.error = '';
      try {
        const apis = selected.map(api => {
          const opts = exportFlow.apis[api.id];
          const entry = { name: api.name.trim(), spec_url: api.specUrl.trim() };
          if (api.baseUrl?.trim()) entry.base_url_override = api.baseUrl.trim();
          if (api.type)            entry.spec_type = api.type;
          const mrb = parseInt(api.maxResponseBytes, 10);
          if (!isNaN(mrb) && mrb > 0) entry.max_response_bytes = mrb;
          if (opts.includeAuth && api.authType && api.authType !== 'none') {
            entry.auth = { type: api.authType };
            if (api.authType === 'bearer')  entry.auth.token = api.bearerToken;
            if (api.authType === 'basic')   { entry.auth.username = api.basicUser; entry.auth.password = api.basicPass; }
            if (api.authType === 'api-key') { entry.auth.header = api.apiKeyHeader; entry.auth.value = api.apiKeyValue; }
            if (api.authType === 'oauth2')  { entry.auth.client_id = api.oauthClientId; entry.auth.client_secret = api.oauthClientSecret; entry.auth.refresh_token = api.oauthRefreshToken; }
          }
          if (opts.includeFilter && api.filterMode && api.filterOperations.length > 0) {
            entry.filter = { mode: api.filterMode, operations: api.filterOperations };
          }
          return entry;
        });

        const manifest = { version: 1, exported_at: new Date().toISOString(), source_profile: activeProfile.value, encrypted: exportFlow.encrypt };
        if (exportFlow.encrypt) {
          const enc = await profileCrypto.encryptApis(apis, exportFlow.password);
          Object.assign(manifest, { kdf: 'pbkdf2-sha256', iterations: 200000, ...enc });
        } else {
          manifest.apis = apis;
        }

        const blob = new Blob([JSON.stringify(manifest, null, 2)], { type: 'application/json' });
        const url  = URL.createObjectURL(blob);
        const a    = document.createElement('a');
        a.href     = url;
        a.download = `${activeProfile.value}.skylineprofile`;
        a.click();
        URL.revokeObjectURL(url);
        closeExportFlow();
      } catch (err) {
        exportFlow.error = `Export failed: ${err.message}`;
      } finally {
        exportFlow.busy = false;
      }
    }

    // ── Import ──────────────────────────────────────────────────────────────
    function openImportFlow() {
      importFlow.open = true;
      importFlow.step = 'pick';
      importFlow.file = null;
      importFlow.targetProfile = profiles.value[0] || '__new__';
      importFlow.newProfileName = '';
      importFlow.password = '';
      importFlow.showPassword = false;
      importFlow.error = '';
      importFlow.importApis = [];
    }

    function closeImportFlow() {
      importFlow.open = false;
      importFlow.password = '';
    }

    async function handleImportFilePick(event) {
      const file = event.target.files[0];
      event.target.value = '';
      if (!file) return;
      importFlow.error = '';
      try {
        const text = await file.text();
        const parsed = JSON.parse(text);
        if (parsed.version !== 1 || !parsed.exported_at) throw new Error('Not a valid .skylineprofile file.');
        importFlow.file = parsed;
        if (parsed.encrypted) {
          importFlow.step = 'password';
        } else {
          await prepareImportApis(parsed.apis);
          importFlow.step = 'merge';
        }
      } catch (err) {
        importFlow.error = `Failed to read file: ${err.message}`;
      }
    }

    async function handleImportDecrypt() {
      if (!importFlow.password) { importFlow.error = 'Password is required.'; return; }
      importFlow.busy = true;
      importFlow.error = '';
      try {
        const apis = await profileCrypto.decryptApis(importFlow.file, importFlow.password);
        await prepareImportApis(apis);
        importFlow.step = 'merge';
      } catch (_) {
        importFlow.error = 'Wrong password or corrupted file.';
      } finally {
        importFlow.busy = false;
      }
    }

    async function prepareImportApis(apis) {
      let existingNames = new Set();
      const target = importFlow.targetProfile;
      if (target && target !== '__new__') {
        try {
          const data = await apiClient.loadProfile(target);
          existingNames = new Set((data.config?.apis || []).map(a => a.name));
        } catch (_) { /* ignore — treat as empty */ }
      }
      importFlow.importApis = apis.map(api => ({
        name: api.name,
        spec_url: api.spec_url || '',
        hasAuth:    !!api.auth,
        hasFilter:  !!(api.filter?.mode),
        selected:     true,
        importAuth:   !!api.auth,
        importFilter: !!(api.filter?.mode),
        conflicts:    existingNames.has(api.name),
        _raw: api,
      }));
    }

    async function onImportTargetChange() {
      if (!importFlow.importApis.length) return;
      let existingNames = new Set();
      const target = importFlow.targetProfile;
      if (target && target !== '__new__') {
        try {
          const data = await apiClient.loadProfile(target);
          existingNames = new Set((data.config?.apis || []).map(a => a.name));
        } catch (_) { /* ignore */ }
      }
      for (const row of importFlow.importApis) {
        row.conflicts = existingNames.has(row.name);
      }
    }

    async function runImport() {
      const selectedRows = importFlow.importApis.filter(r => r.selected);
      if (selectedRows.length === 0) { importFlow.error = 'Select at least one API.'; return; }
      const profileName = importFlow.targetProfile === '__new__'
        ? importFlow.newProfileName.trim()
        : importFlow.targetProfile;
      if (!profileName) { importFlow.error = 'Profile name is required.'; return; }
      importFlow.busy = true;
      importFlow.error = '';
      try {
        let existingApis = [];
        let existingToken = '';
        if (importFlow.targetProfile !== '__new__') {
          try {
            const data = await apiClient.loadProfile(profileName);
            existingApis = data.config?.apis || [];
            existingToken = data.token || '';
          } catch (_) { /* new */ }
        }
        if (!existingToken) {
          const arr = new Uint8Array(32);
          crypto.getRandomValues(arr);
          existingToken = btoa(String.fromCharCode(...arr));
        }
        const merged = [...existingApis];
        for (const row of selectedRows) {
          const api = { ...row._raw };
          if (!row.importAuth)   delete api.auth;
          if (!row.importFilter) delete api.filter;
          const idx = merged.findIndex(e => e.name === api.name);
          if (idx >= 0) merged[idx] = api; else merged.push(api);
        }
        await apiClient.saveProfile(profileName, existingToken, { apis: merged });
        await refreshProfiles();
        await loadProfile(profileName);
        closeImportFlow();
        setStatus('ok', `Imported ${selectedRows.length} API(s) into "${profileName}".`);
      } catch (err) {
        importFlow.error = `Import failed: ${err.message}`;
      } finally {
        importFlow.busy = false;
      }
    }

    return {
      profiles,
      activeProfile,
      profileMetadata,
      form,
      draft,
      status,
      isBusy,
      typeIcons,
      typeLabels,
      refreshProfiles,
      loadProfile,
      removeApi,
      detectApi,
      detectOnBlur,
      detectDraft,
      detectDraftOnBlur,
      selectDetectedOption,
      addDraftToList,
      testApi,
      saveProfile,
      openNewProfileModal,
      createNewProfile,
      closeNewProfileModal,
      showNewProfileModal,
      newProfileName,
      newProfileError,
      deleteProfile,
      addedToast,
      toggleFilterConfig,
      fetchOperations,
      toggleOperationSelection,
      onFilterModeChange,
      clearFilter,
      groupOperations,
      toggleGroup,
      toggleGroupSelection,
      getFilterView,
      allMethodsForApi,
      isMethodActive,
      toggleMethodFilter,
      filteredOperations,
      filteredGroups,
      selectVisible,
      deselectVisible,
      clearSearch,
      showToken,
      toggleTokenVisibility,
      serviceIcons,
      serviceLabels,
      slackTokenType,
      applySimpleToken,
      handleKubeconfigUpload,
      // Add API flow
      addFlow,
      openAddFlow,
      closeAddFlow,
      pickService,
      handleAddFlowKubeUpload,
      addKubernetes,
      addGitLab,
      addJira,
      addSlack,
      addGmail,
      oauthRedirectHint,
      runAddFlowDetect,
      addFromDetectResult,
      loadLibrary,
      libraryCategories,
      filteredLibraryItems,
      addFromLibrary,
      // Profile tree / tabs
      selectedApiId,
      selectedApi,
      profileTab,
      expandedProfiles,
      profileStats,
      profileMetrics,
      statsLoading,
      selectProfile,
      selectApi,
      backToProfile,
      toggleProfileExpand,
      selectApiBySpecUrl,
      // MCP connect
      connectPanel,
      claudeDesktopSnippet,
      claudeCodeCmd,
      claudeCodeSettings,
      clineSnippet,
      codexSnippet,
      copySnippet,
      // Export / Import
      exportFlow,
      openExportFlow,
      closeExportFlow,
      runExport,
      importFlow,
      openImportFlow,
      closeImportFlow,
      handleImportFilePick,
      handleImportDecrypt,
      onImportTargetChange,
      runImport,
    };
  },
  template: `
    <div class="app">
      <aside class="panel">
        <div class="profile-list">
          <div class="sidebar-header">
            <span class="notice" style="margin:0; padding:0;">Profiles</span>
            <div style="display:flex; gap:2px; align-items:center;">
              <button class="icon-btn" @click="openImportFlow" title="Import Profile">
                <iconify-icon icon="mdi:import"></iconify-icon>
              </button>
              <button
                class="icon-btn"
                @click="openExportFlow"
                :disabled="!activeProfile || !form.apis.length"
                :style="{ opacity: (!activeProfile || !form.apis.length) ? '0.35' : '1' }"
                title="Export Profile"
              >
                <iconify-icon icon="mdi:export"></iconify-icon>
              </button>
              <button class="icon-btn" @click="openNewProfileModal" title="New Profile">
                <iconify-icon icon="mdi:plus-circle-outline"></iconify-icon>
              </button>
            </div>
          </div>

          <div v-if="profiles.length === 0" class="notice" style="margin-top:12px; font-size:12px; opacity:0.7;">No profiles yet.</div>

          <div v-for="name in profiles" :key="name" class="tree-profile">
            <!-- Profile row -->
            <div
              class="tree-profile-row"
              :class="{ active: name === activeProfile && !selectedApiId }"
              @click="selectProfile(name)"
            >
              <button class="tree-expand-btn" @click.stop="toggleProfileExpand(name)">
                <iconify-icon :icon="expandedProfiles[name] ? 'mdi:chevron-down' : 'mdi:chevron-right'"></iconify-icon>
              </button>
              <iconify-icon icon="mdi:folder-account" class="tree-profile-icon"></iconify-icon>
              <span class="tree-profile-name">{{ name }}</span>
              <span v-if="profileMetadata[name]" class="profile-api-count">{{ profileMetadata[name].apiCount }}</span>
            </div>
            <!-- APIs nested under profile -->
            <div v-if="expandedProfiles[name]" class="tree-api-list">
              <div v-if="!profileMetadata[name]?.apis?.length" class="tree-api-empty">No APIs</div>
              <div
                v-for="api in (profileMetadata[name]?.apis || [])"
                :key="api.specUrl"
                class="tree-api-row"
                :class="{ active: name === activeProfile && selectedApi && selectedApi.specUrl === api.specUrl }"
                @click.stop="selectApiBySpecUrl(name, api.specUrl)"
              >
                <iconify-icon :icon="serviceIcons[api.knownService] || typeIcons[api.type] || 'mdi:cloud-outline'" class="tree-api-icon"></iconify-icon>
                <span class="tree-api-name">{{ api.name || api.specUrl }}</span>
              </div>
            </div>
          </div>
        </div>
      </aside>

      <main class="panel">
        <!-- Welcome: no profiles exist -->
        <div v-if="!activeProfile && profiles.length === 0" class="welcome-screen">
          <div class="welcome-content">
            <img src="/ui/skyline-logo.svg" alt="Skyline" style="max-width: 200px; height: auto; margin-bottom: 24px; opacity: 0.8;">
            <h2 class="welcome-title">Welcome to Skyline</h2>
            <p class="welcome-subtitle">Create your first profile to start managing API connections for your MCP servers.</p>
            <button class="primary welcome-cta" @click="openNewProfileModal">
              <iconify-icon icon="mdi:plus-circle-outline"></iconify-icon>
              Create Your First Profile
            </button>
          </div>
        </div>

        <!-- Profiles exist but none selected -->
        <div v-else-if="!activeProfile && profiles.length > 0" class="welcome-screen">
          <div class="welcome-content">
            <iconify-icon icon="mdi:folder-open-outline" style="font-size: 48px; color: var(--text-dim); margin-bottom: 16px;"></iconify-icon>
            <h2 class="welcome-title">Select a Profile</h2>
            <p class="welcome-subtitle">Choose a profile from the sidebar to get started.</p>
            <button class="primary welcome-cta" @click="openNewProfileModal">
              <iconify-icon icon="mdi:plus-circle-outline"></iconify-icon>
              New Profile
            </button>
          </div>
        </div>

        <!-- API Config View -->
        <template v-else-if="selectedApiId && selectedApi">
          <div class="view-header">
            <button class="ghost small" @click="backToProfile" style="display:flex;align-items:center;gap:6px;">
              <iconify-icon icon="mdi:arrow-left"></iconify-icon>
              {{ activeProfile }}
            </button>
            <div class="view-title">
              <iconify-icon :icon="serviceIcons[selectedApi.knownService] || typeIcons[selectedApi.type] || 'mdi:cloud-outline'" style="font-size:20px;"></iconify-icon>
              {{ selectedApi.name || selectedApi.specUrl }}
            </div>
          </div>
          <div class="tab-content">
            <div class="api-card">
              <div class="api-header">
                <div class="api-type">
                  <iconify-icon :icon="serviceIcons[selectedApi.knownService] || typeIcons[selectedApi.type] || 'mdi:cloud-outline'"></iconify-icon>
                  <div>
                    <div>{{ selectedApi.knownService ? serviceLabels[selectedApi.knownService] : (selectedApi.type ? typeLabels[selectedApi.type] : "Unknown type") }}</div>
                    <div class="muted">{{ selectedApi.name || selectedApi.specUrl }}</div>
                  </div>
                </div>
                <div class="api-actions">
                  <button class="secondary" :disabled="isBusy" @click="detectApi(selectedApi)">Re-detect</button>
                  <button class="secondary" :disabled="isBusy" @click="testApi(selectedApi)">Test</button>
                  <button class="ghost" @click="removeApi(selectedApi.id); backToProfile()">Remove</button>
                </div>
              </div>

              <!-- Known services: show connection info as read-only -->
              <div v-if="selectedApi.knownService" class="form-grid">
                <div>
                  <label>Base URL</label>
                  <input :value="selectedApi.baseUrl" readonly class="input-readonly" />
                </div>
                <div>
                  <label>API name</label>
                  <input v-model="selectedApi.name" placeholder="domain - API type" />
                </div>
              </div>

              <!-- Known-service credential helpers (shown instead of generic auth) -->
              <div v-if="selectedApi.knownService === 'gitlab'" class="credential-helper gitlab-helper">
                <div class="helper-header"><iconify-icon icon="simple-icons:gitlab"></iconify-icon>GitLab Authentication</div>
                <p class="helper-desc">Personal Access Token (requires <code>api</code> scope).</p>
                <div style="position:relative;">
                  <input :type="selectedApi.showSecret ? 'text' : 'password'" placeholder="glpat-xxxxxxxxxxxxxxxxxxxx" :value="selectedApi.bearerToken"
                    @input="selectedApi.authType='bearer'; selectedApi.bearerToken=$event.target.value" style="width:100%; padding-right:40px;" />
                  <button class="token-toggle" @click="selectedApi.showSecret = !selectedApi.showSecret" type="button" title="Toggle token visibility">
                    <iconify-icon :icon="selectedApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon>
                  </button>
                </div>
              </div>
              <div v-if="selectedApi.knownService === 'jira'" class="credential-helper jira-helper">
                <div class="helper-header"><iconify-icon icon="simple-icons:jira"></iconify-icon>Jira Authentication</div>
                <template v-if="selectedApi.authType === 'basic'">
                  <p class="helper-desc">Email &amp; API Token (Jira Cloud).</p>
                  <div class="form-grid">
                    <div><label>Email</label><input v-model="selectedApi.basicUser" placeholder="you@company.com" /></div>
                    <div>
                      <label>API Token</label>
                      <div style="position:relative;">
                        <input :type="selectedApi.showSecret ? 'text' : 'password'" placeholder="Your Jira API token" :value="selectedApi.basicPass"
                          @input="selectedApi.basicPass=$event.target.value" style="width:100%; padding-right:40px;" />
                        <button class="token-toggle" @click="selectedApi.showSecret = !selectedApi.showSecret" type="button" title="Toggle token visibility">
                          <iconify-icon :icon="selectedApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon>
                        </button>
                      </div>
                    </div>
                  </div>
                </template>
                <template v-else>
                  <p class="helper-desc">Personal Access Token.</p>
                  <div style="position:relative;">
                    <input :type="selectedApi.showSecret ? 'text' : 'password'" placeholder="Your Jira PAT" :value="selectedApi.bearerToken"
                      @input="selectedApi.authType='bearer'; selectedApi.bearerToken=$event.target.value" style="width:100%; padding-right:40px;" />
                    <button class="token-toggle" @click="selectedApi.showSecret = !selectedApi.showSecret" type="button" title="Toggle token visibility">
                      <iconify-icon :icon="selectedApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon>
                    </button>
                  </div>
                </template>
              </div>
              <div v-if="selectedApi.knownService === 'slack'" class="credential-helper slack-helper">
                <div class="helper-header"><iconify-icon icon="simple-icons:slack"></iconify-icon>Slack Authentication</div>
                <p class="helper-desc">
                  <span v-if="slackTokenType(selectedApi.bearerToken) === 'bot'">Bot token detected</span>
                  <span v-else-if="slackTokenType(selectedApi.bearerToken) === 'user'">User token detected</span>
                  <span v-else>Enter an <code>xoxb-</code> Bot Token or <code>xoxp-</code> User Token</span>
                </p>
                <div style="position:relative;">
                  <input :type="selectedApi.showSecret ? 'text' : 'password'" placeholder="xoxb-... or xoxp-..." :value="selectedApi.bearerToken"
                    @input="selectedApi.authType='bearer'; selectedApi.bearerToken=$event.target.value" style="width:100%; padding-right:40px;" />
                  <button class="token-toggle" @click="selectedApi.showSecret = !selectedApi.showSecret" type="button" title="Toggle token visibility">
                    <iconify-icon :icon="selectedApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon>
                  </button>
                </div>
              </div>

              <div v-if="selectedApi.knownService === 'gmail'" class="credential-helper gmail-helper">
                <div class="helper-header"><iconify-icon icon="simple-icons:gmail"></iconify-icon>Gmail Authentication (OAuth 2.0)</div>
                <div v-if="selectedApi.oauthConnected" style="display:flex; align-items:center; gap:6px; margin:8px 0;">
                  <iconify-icon icon="mdi:check-circle" style="color:var(--green); font-size:16px;"></iconify-icon>
                  <span>Connected<span v-if="selectedApi.oauthEmail"> as {{ selectedApi.oauthEmail }}</span></span>
                </div>
                <div v-else style="display:flex; align-items:center; gap:6px; margin:8px 0;">
                  <iconify-icon icon="mdi:alert-circle" style="color:var(--red); font-size:16px;"></iconify-icon>
                  <span>Not connected — re-add this API to authorize</span>
                </div>
                <p class="helper-desc" style="margin-top:4px;">OAuth credentials are encrypted in the profile. Tokens refresh automatically.</p>
              </div>

              <!-- Generic APIs: full editable connection fields -->
              <template v-if="!selectedApi.knownService">
                <div class="form-grid">
                  <div>
                    <label>Base URL</label>
                    <input v-model="selectedApi.baseUrl" placeholder="http://localhost:9999" @blur="detectOnBlur(selectedApi)" />
                  </div>
                  <div>
                    <label>API name</label>
                    <input v-model="selectedApi.name" placeholder="domain - API type" />
                  </div>
                </div>

                <div class="form-grid">
                  <div>
                    <label>Spec URL</label>
                    <input v-model="selectedApi.specUrl" placeholder="autofilled after detect" />
                  </div>
                  <div>
                    <label>Auth type</label>
                    <select v-model="selectedApi.authType">
                      <option value="none">None</option>
                      <option value="bearer">Bearer</option>
                      <option value="basic">Basic</option>
                      <option value="api-key">API Key</option>
                    </select>
                  </div>
                </div>

                <div v-if="selectedApi.detectedOptions.length > 1" class="form-grid">
                  <div>
                    <label>Detected types</label>
                    <select @change="selectDetectedOption(selectedApi, selectedApi.detectedOptions[$event.target.selectedIndex])">
                      <option v-for="opt in selectedApi.detectedOptions" :key="opt.spec_url">
                        {{ typeLabels[opt.type] || opt.type }} — {{ opt.spec_url }}
                      </option>
                    </select>
                  </div>
                </div>

                <div v-if="selectedApi.authType === 'bearer'" class="form-grid">
                  <div>
                    <label>Bearer token</label>
                    <div style="position:relative;">
                      <input v-model="selectedApi.bearerToken" :type="selectedApi.showSecret ? 'text' : 'password'" style="width:100%; padding-right:40px;" />
                      <button class="token-toggle" @click="selectedApi.showSecret = !selectedApi.showSecret" type="button" title="Toggle token visibility">
                        <iconify-icon :icon="selectedApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon>
                      </button>
                    </div>
                  </div>
                </div>
                <div v-if="selectedApi.authType === 'basic'" class="form-grid">
                  <div><label>Username</label><input v-model="selectedApi.basicUser" /></div>
                  <div>
                    <label>Password</label>
                    <div style="position:relative;">
                      <input v-model="selectedApi.basicPass" :type="selectedApi.showSecret ? 'text' : 'password'" style="width:100%; padding-right:40px;" />
                      <button class="token-toggle" @click="selectedApi.showSecret = !selectedApi.showSecret" type="button" title="Toggle token visibility">
                        <iconify-icon :icon="selectedApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon>
                      </button>
                    </div>
                  </div>
                </div>
                <div v-if="selectedApi.authType === 'api-key'" class="form-grid">
                  <div><label>Header</label><input v-model="selectedApi.apiKeyHeader" /></div>
                  <div>
                    <label>Value</label>
                    <div style="position:relative;">
                      <input v-model="selectedApi.apiKeyValue" :type="selectedApi.showSecret ? 'text' : 'password'" style="width:100%; padding-right:40px;" />
                      <button class="token-toggle" @click="selectedApi.showSecret = !selectedApi.showSecret" type="button" title="Toggle token visibility">
                        <iconify-icon :icon="selectedApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon>
                      </button>
                    </div>
                  </div>
                </div>
              </template>

              <!-- Response Truncation -->
              <div class="form-grid" style="margin-top:12px;">
                <div>
                  <label>Max Response Size</label>
                  <input v-model="selectedApi.maxResponseBytes" type="number" min="0" step="1024" placeholder="Default (51200 = 50KB)" />
                  <div class="muted" style="font-size:11px; margin-top:4px;">
                    Bytes. 0 = no limit. Empty = inherit global default (50KB).
                  </div>
                </div>
              </div>

              <!-- Filter Configuration -->
              <div class="filter-section">
                <div class="filter-header">
                  <div class="filter-info">
                    <div class="filter-title"><iconify-icon icon="mdi:filter-variant"></iconify-icon> Operation Filter</div>
                    <div class="filter-status" :class="{ active: selectedApi.filterMode }">
                      <span v-if="!selectedApi.filterMode">No filter — all operations allowed</span>
                      <span v-else><strong>{{ selectedApi.filterMode === 'allowlist' ? 'Allowlist' : 'Blocklist' }}</strong> · {{ selectedApi.filterOperations.length }} pattern{{ selectedApi.filterOperations.length !== 1 ? 's' : '' }}</span>
                    </div>
                  </div>
                  <div class="filter-actions">
                    <button class="secondary" @click="toggleFilterConfig(selectedApi)">
                      <iconify-icon :icon="selectedApi.showFilterConfig ? 'mdi:chevron-up' : 'mdi:tune'"></iconify-icon>
                      {{ selectedApi.showFilterConfig ? 'Hide' : 'Configure' }}
                    </button>
                    <button v-if="selectedApi.filterMode" class="ghost" @click="clearFilter(selectedApi)">
                      <iconify-icon icon="mdi:close"></iconify-icon> Clear
                    </button>
                  </div>
                </div>
                <div v-if="selectedApi.showFilterConfig" class="filter-config-panel">
                  <div class="filter-mode-selector">
                    <label><iconify-icon icon="mdi:shield-check"></iconify-icon> Filter Mode</label>
                    <select v-model="selectedApi.filterMode" @change="onFilterModeChange(selectedApi)">
                      <option value="">Choose filter strategy...</option>
                      <option value="allowlist">✓ Allowlist — Only selected operations allowed</option>
                      <option value="blocklist">✗ Blocklist — Selected operations blocked</option>
                    </select>
                  </div>
                  <div v-if="selectedApi.filterLoading" class="loading-state">
                    <iconify-icon icon="mdi:loading"></iconify-icon>
                    <div style="margin-top:12px;">Loading operations...</div>
                  </div>
                  <div v-else-if="selectedApi.availableOperations.length > 0">
                    <!-- Filter Toolbar: method pills + search + bulk actions -->
                    <div class="filter-toolbar">
                      <div class="method-pills">
                        <button
                          v-for="method in allMethodsForApi(selectedApi)"
                          :key="method"
                          class="method-pill"
                          :class="[method.toLowerCase(), { inactive: !isMethodActive(selectedApi, method) }]"
                          @click="toggleMethodFilter(selectedApi, method)"
                        >{{ method }}</button>
                      </div>
                      <div class="filter-search-wrapper">
                        <iconify-icon icon="mdi:magnify" class="filter-search-icon"></iconify-icon>
                        <input
                          type="text"
                          class="filter-search-input"
                          placeholder="Search by name, path, or description..."
                          :value="getFilterView(selectedApi).searchQuery"
                          @input="getFilterView(selectedApi).searchQuery = $event.target.value"
                        />
                        <button v-if="getFilterView(selectedApi).searchQuery" class="filter-search-clear" @click="clearSearch(selectedApi)">
                          <iconify-icon icon="mdi:close-circle"></iconify-icon>
                        </button>
                      </div>
                      <div class="filter-bulk-actions">
                        <span class="filter-visible-count">
                          {{ filteredOperations(selectedApi).length }} of {{ selectedApi.availableOperations.length }} visible
                          · {{ selectedApi.selectedOperations.size }} selected
                        </span>
                        <button class="ghost small" @click="selectVisible(selectedApi)">
                          <iconify-icon icon="mdi:checkbox-multiple-marked"></iconify-icon> Select visible
                        </button>
                        <button class="ghost small" @click="deselectVisible(selectedApi)">
                          <iconify-icon icon="mdi:checkbox-multiple-blank-outline"></iconify-icon> Deselect visible
                        </button>
                      </div>
                    </div>

                    <div class="operations-list">
                      <div v-for="group in filteredGroups(selectedApi)" :key="group.name" class="op-group">
                        <div class="op-group-header" @click="toggleGroup(selectedApi, group.name)">
                          <input type="checkbox" class="operation-checkbox"
                            :checked="group.operations.every(op => selectedApi.selectedOperations.has(op.id))"
                            :indeterminate.prop="group.operations.some(op => selectedApi.selectedOperations.has(op.id)) && !group.operations.every(op => selectedApi.selectedOperations.has(op.id))"
                            @click.stop="toggleGroupSelection(selectedApi, group.operations)" />
                          <iconify-icon :icon="selectedApi.collapsedGroups?.has(group.name) ? 'mdi:chevron-right' : 'mdi:chevron-down'" class="op-group-chevron"></iconify-icon>
                          <span class="op-group-name">{{ group.name }}</span>
                          <span class="op-group-count">{{ group.operations.filter(op => selectedApi.selectedOperations.has(op.id)).length }}/{{ group.operations.length }}</span>
                        </div>
                        <div v-if="!selectedApi.collapsedGroups?.has(group.name)" class="op-group-body">
                          <div
                            v-for="op in group.operations"
                            :key="op.id"
                            class="operation-item"
                            :class="{ selected: selectedApi.selectedOperations.has(op.id) }"
                            @click="toggleOperationSelection(selectedApi, op.id)"
                          >
                            <input type="checkbox" class="operation-checkbox" :checked="selectedApi.selectedOperations.has(op.id)" @click.stop="toggleOperationSelection(selectedApi, op.id)" />
                            <div class="operation-content">
                              <div class="operation-header">
                                <span class="method-badge" :class="op.method.toLowerCase()">{{ op.method }}</span>
                                <span class="operation-id">{{ op.id }}</span>
                              </div>
                              <div class="operation-path">{{ op.path }}</div>
                              <div v-if="op.summary" class="operation-summary">{{ op.summary }}</div>
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                    <div class="filter-live-preview" v-if="selectedApi.filterMode && selectedApi.selectedOperations.size > 0">
                      <iconify-icon icon="mdi:check-circle"></iconify-icon>
                      <span v-if="selectedApi.filterMode === 'allowlist'">Exposing <strong>{{ selectedApi.selectedOperations.size }}</strong> of {{ selectedApi.availableOperations.length }} operations</span>
                      <span v-else>Blocking <strong>{{ selectedApi.selectedOperations.size }}</strong>, exposing {{ selectedApi.availableOperations.length - selectedApi.selectedOperations.size }}</span>
                    </div>
                  </div>
                  <div v-else class="empty-state">
                    <iconify-icon icon="mdi:file-document-outline"></iconify-icon>
                    <div style="margin-top:8px; font-weight:500;">No operations loaded yet</div>
                  </div>
                </div>
              </div>
            </div>

            <div class="toolbar" style="margin-top:12px;">
              <div v-if="status.message" class="status">
                <span class="status-dot" :class="{ ok: status.state === 'ok', err: status.state === 'error' }"></span>
                <span>{{ status.message }}</span>
              </div>
              <span v-else style="font-size:12px; color:var(--text-dim);">Changes auto-save</span>
            </div>
          </div>

        </template>

        <!-- Profile View: profile selected, no API -->
        <template v-else>
          <div class="view-header">
            <div class="view-title">
              <iconify-icon icon="mdi:folder-account" style="font-size:20px;"></iconify-icon>
              {{ activeProfile }}
            </div>
          </div>
          <div class="tab-bar">
            <button :class="['tab-btn', { active: profileTab === 'overview' }]" @click="profileTab = 'overview'">Overview</button>
            <button :class="['tab-btn', { active: profileTab === 'apis' }]" @click="profileTab = 'apis'">APIs</button>
            <button :class="['tab-btn', { active: profileTab === 'settings' }]" @click="profileTab = 'settings'">Settings</button>
          </div>

          <!-- Overview tab: always-visible dashboard -->
          <div v-if="profileTab === 'overview'" class="tab-content">
            <div v-if="statsLoading" class="loading-state" style="padding:40px; text-align:center;">
              <iconify-icon icon="mdi:loading" style="font-size:32px; color:var(--text-dim);"></iconify-icon>
              <div style="margin-top:12px; color:var(--text-dim);">Loading stats...</div>
            </div>
            <div v-else>
              <!-- Connection & profile summary -->
              <div style="display:flex; gap:12px; margin-bottom:16px; flex-wrap:wrap;">
                <div style="flex:1; min-width:200px; background:var(--bg-card); border:1px solid var(--border); border-radius:var(--radius); padding:14px 16px; display:flex; align-items:center; gap:12px;">
                  <div :style="{ width:'10px', height:'10px', borderRadius:'50%', background: (profileMetrics && profileMetrics.active_connections > 0) ? 'var(--green)' : 'var(--text-dim)', boxShadow: (profileMetrics && profileMetrics.active_connections > 0) ? '0 0 8px var(--green)' : 'none' }"></div>
                  <div>
                    <div style="font-weight:600; font-size:14px;">{{ (profileMetrics && profileMetrics.active_connections > 0) ? 'Agent Connected' : 'No Agent Connected' }}</div>
                    <div class="muted" style="font-size:12px;">{{ (profileMetrics && profileMetrics.active_connections) || 0 }} active {{ (profileMetrics && profileMetrics.active_connections === 1) ? 'connection' : 'connections' }}</div>
                  </div>
                </div>
                <div style="display:flex; gap:12px;">
                  <div style="background:var(--bg-card); border:1px solid var(--border); border-radius:var(--radius); padding:14px 16px; text-align:center; min-width:90px;">
                    <div style="font-size:22px; font-weight:700;">{{ form.apis.length }}</div>
                    <div class="muted" style="font-size:11px; text-transform:uppercase; letter-spacing:0.5px;">APIs</div>
                  </div>
                  <div style="background:var(--bg-card); border:1px solid var(--border); border-radius:var(--radius); padding:14px 16px; text-align:center; min-width:90px;">
                    <div style="font-size:22px; font-weight:700;">{{ (profileMetrics && profileMetrics.total_connections) || 0 }}</div>
                    <div class="muted" style="font-size:11px; text-transform:uppercase; letter-spacing:0.5px;">Sessions</div>
                  </div>
                </div>
              </div>

              <!-- Metrics grid — always visible, zeros when empty -->
              <div class="stats-grid" style="grid-template-columns: repeat(4, 1fr);">
                <div class="stat-card">
                  <div class="stat-value">{{ (profileStats && profileStats.total_requests) || 0 }}</div>
                  <div class="stat-label">Tool Calls (24h)</div>
                </div>
                <div class="stat-card" :class="{ 'stat-error': profileStats && profileStats.error_rate > 10 }">
                  <div class="stat-value">{{ profileStats && profileStats.total_requests > 0 ? profileStats.error_rate.toFixed(1) + '%' : '0%' }}</div>
                  <div class="stat-label">Error Rate</div>
                </div>
                <div class="stat-card">
                  <div class="stat-value">{{ profileStats && profileStats.avg_duration_ms > 0 ? profileStats.avg_duration_ms + 'ms' : '--' }}</div>
                  <div class="stat-label">Avg Latency</div>
                </div>
                <div class="stat-card">
                  <div class="stat-value">{{ (profileStats && profileStats.successful_requests) || 0 }}</div>
                  <div class="stat-label">Successful</div>
                </div>
              </div>

              <!-- Token usage -->
              <div class="stats-grid" style="grid-template-columns: repeat(2, 1fr);">
                <div class="stat-card">
                  <div class="stat-value" style="font-size:22px;">{{ (profileStats && profileStats.est_request_tokens) ? profileStats.est_request_tokens.toLocaleString() : '0' }}</div>
                  <div class="stat-label">Est. Tokens In</div>
                </div>
                <div class="stat-card">
                  <div class="stat-value" style="font-size:22px;">{{ (profileStats && profileStats.est_response_tokens) ? profileStats.est_response_tokens.toLocaleString() : '0' }}</div>
                  <div class="stat-label">Est. Tokens Out</div>
                </div>
              </div>

              <!-- Top APIs -->
              <div class="stats-section">
                <div class="stats-section-title">Top APIs</div>
                <div v-if="profileStats && profileStats.top_apis && profileStats.top_apis.length > 0" class="stats-table">
                  <div class="stats-row header"><span>API</span><span>Calls</span><span>Errors</span><span>Avg ms</span></div>
                  <div v-for="a in profileStats.top_apis" :key="a.name" class="stats-row">
                    <span>{{ a.name }}</span><span>{{ a.calls }}</span><span>{{ a.errors }}</span><span>{{ a.avg_ms }}</span>
                  </div>
                </div>
                <div v-else style="background:var(--bg-card); border:1px solid var(--border); border-radius:var(--radius); padding:16px; text-align:center; color:var(--text-dim); font-size:13px;">
                  API usage will appear here when agents start making calls
                </div>
              </div>

              <!-- Top Tools -->
              <div class="stats-section">
                <div class="stats-section-title">Top Tools</div>
                <div v-if="profileStats && profileStats.top_tools && profileStats.top_tools.length > 0" class="stats-table">
                  <div class="stats-row header"><span>Tool</span><span>Calls</span><span>Errors</span><span>Avg ms</span></div>
                  <div v-for="t in profileStats.top_tools" :key="t.name" class="stats-row">
                    <span>{{ t.name }}</span><span>{{ t.calls }}</span><span>{{ t.errors }}</span><span>{{ t.avg_ms }}</span>
                  </div>
                </div>
                <div v-else style="background:var(--bg-card); border:1px solid var(--border); border-radius:var(--radius); padding:16px; text-align:center; color:var(--text-dim); font-size:13px;">
                  Tool call breakdown will appear here after first usage
                </div>
              </div>

              <!-- Recent Activity -->
              <div class="stats-section">
                <div class="stats-section-title">Recent Activity</div>
                <div v-if="profileStats && profileStats.recent_events && profileStats.recent_events.length > 0">
                  <div v-for="e in profileStats.recent_events.slice(0, 10)" :key="e.id" class="event-row">
                    <span class="event-dot" :class="{ ok: e.success, err: !e.success }"></span>
                    <span class="event-tool">{{ e.tool_name || e.event_type }}</span>
                    <span class="event-api muted">{{ e.api_name }}</span>
                    <span v-if="e.request_size || e.response_size" class="muted" style="font-size:11px; color:#FBBF24;">~{{ Math.round((e.request_size || 0) / 4) }}/{{ Math.round((e.response_size || 0) / 4) }} tok</span>
                    <span class="event-dur muted">{{ e.duration_ms }}ms</span>
                  </div>
                </div>
                <div v-else style="background:var(--bg-card); border:1px solid var(--border); border-radius:var(--radius); padding:16px; text-align:center; color:var(--text-dim); font-size:13px;">
                  Activity log will stream here as agents call tools
                </div>
              </div>
            </div>
          </div>

          <!-- APIs tab -->
          <div v-else-if="profileTab === 'apis'" class="tab-content">
            <div style="display:flex; align-items:center; justify-content:space-between; margin-bottom:12px;">
              <div class="step-pill">APIs in Profile</div>
              <div style="display:flex; align-items:center; gap:12px;">
                <div v-if="addedToast" class="toast fade-in">Added to profile</div>
                <button class="primary" @click="openAddFlow">
                  <iconify-icon icon="mdi:plus-circle-outline"></iconify-icon>
                  Add API
                </button>
              </div>
            </div>
            <div v-if="form.apis.length === 0" style="text-align:center; padding:40px 20px; color:var(--text-dim); opacity:0.6;">
              No APIs added yet
            </div>
            <div
              v-for="api in form.apis"
              :key="api.id"
              class="api-card api-card-list"
              @click="selectApi(api.id)"
              style="cursor:pointer;"
            >
              <div class="api-header">
                <div class="api-type">
                  <iconify-icon :icon="serviceIcons[api.knownService] || typeIcons[api.type] || 'mdi:cloud-outline'"></iconify-icon>
                  <div>
                    <div>{{ api.knownService ? serviceLabels[api.knownService] : (api.type ? typeLabels[api.type] : "Unknown type") }}</div>
                    <div class="muted">{{ api.name || api.specUrl }}</div>
                  </div>
                </div>
                <div class="api-actions">
                  <button class="ghost" @click.stop="removeApi(api.id)">Remove</button>
                  <iconify-icon icon="mdi:chevron-right" style="color:var(--text-dim); font-size:18px;"></iconify-icon>
                </div>
              </div>
            </div>
          </div>

          <!-- Settings tab -->
          <div v-else-if="profileTab === 'settings'" class="tab-content">
            <div class="profile-header-card">
              <div class="form-grid">
                <div>
                  <label>Profile name</label>
                  <input v-model="form.profileName" placeholder="dev, prod, agent-a" />
                </div>
                <div>
                  <label>Profile token</label>
                  <div v-if="form.profileToken" style="position:relative;">
                    <input :value="form.profileToken" :type="showToken ? 'text' : 'password'" disabled style="width:100%; padding-right:40px;" />
                    <button class="token-toggle" @click="toggleTokenVisibility" type="button" title="Toggle token visibility">
                      <iconify-icon :icon="showToken ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon>
                    </button>
                  </div>
                  <div v-else class="token-placeholder">
                    <iconify-icon icon="mdi:information-outline"></iconify-icon>
                    <span>Save profile to generate token</span>
                  </div>
                </div>
              </div>
            </div>
            <!-- MCP client connect section -->
            <div v-if="form.profileToken" class="connect-section">
              <div class="connect-section-heading">
                <iconify-icon icon="mdi:connection"></iconify-icon>
                <span>Connect your AI client</span>
              </div>

              <!-- Claude Desktop -->
              <div class="connect-panel">
                <div class="kube-tutorial-toggle" @click="connectPanel = connectPanel === 'claude-desktop' ? '' : 'claude-desktop'">
                  <iconify-icon :icon="connectPanel === 'claude-desktop' ? 'mdi:chevron-down' : 'mdi:chevron-right'"></iconify-icon>
                  <iconify-icon icon="simple-icons:anthropic" style="font-size:13px; color:#d97757;"></iconify-icon>
                  <span>Claude Desktop</span>
                </div>
                <div v-if="connectPanel === 'claude-desktop'" class="kube-tutorial">
                  <p>Add to <code>~/Library/Application Support/Claude/claude_desktop_config.json</code> (macOS) or <code>%APPDATA%\Claude\claude_desktop_config.json</code> (Windows):</p>
                  <pre>{{ claudeDesktopSnippet }}</pre>
                  <div class="tutorial-actions">
                    <button class="ghost" style="font-size:11px; padding:3px 8px;" @click="copySnippet(claudeDesktopSnippet)">
                      <iconify-icon icon="mdi:content-copy"></iconify-icon> Copy
                    </button>
                  </div>
                </div>
              </div>

              <!-- Claude Code -->
              <div class="connect-panel">
                <div class="kube-tutorial-toggle" @click="connectPanel = connectPanel === 'claude-code' ? '' : 'claude-code'">
                  <iconify-icon :icon="connectPanel === 'claude-code' ? 'mdi:chevron-down' : 'mdi:chevron-right'"></iconify-icon>
                  <iconify-icon icon="mdi:console-line" style="font-size:13px; color:#d97757;"></iconify-icon>
                  <span>Claude Code (CLI)</span>
                </div>
                <div v-if="connectPanel === 'claude-code'" class="kube-tutorial">
                  <p>Run in your terminal:</p>
                  <pre>{{ claudeCodeCmd }}</pre>
                  <div class="tutorial-actions">
                    <button class="ghost" style="font-size:11px; padding:3px 8px;" @click="copySnippet(claudeCodeCmd)">
                      <iconify-icon icon="mdi:content-copy"></iconify-icon> Copy
                    </button>
                  </div>
                  <p>Or add to <code>~/.claude/settings.json</code>:</p>
                  <pre>{{ claudeCodeSettings }}</pre>
                  <div class="tutorial-actions">
                    <button class="ghost" style="font-size:11px; padding:3px 8px;" @click="copySnippet(claudeCodeSettings)">
                      <iconify-icon icon="mdi:content-copy"></iconify-icon> Copy
                    </button>
                  </div>
                </div>
              </div>

              <!-- Cline (VS Code) -->
              <div class="connect-panel">
                <div class="kube-tutorial-toggle" @click="connectPanel = connectPanel === 'cline' ? '' : 'cline'">
                  <iconify-icon :icon="connectPanel === 'cline' ? 'mdi:chevron-down' : 'mdi:chevron-right'"></iconify-icon>
                  <iconify-icon icon="mdi:puzzle-outline" style="font-size:13px; color:#8b5cf6;"></iconify-icon>
                  <span>Cline (VS Code)</span>
                </div>
                <div v-if="connectPanel === 'cline'" class="kube-tutorial">
                  <p>In Cline, go to <strong>MCP Servers → Add Server</strong>, or edit <code>cline_mcp_settings.json</code>:</p>
                  <pre>{{ clineSnippet }}</pre>
                  <div class="tutorial-actions">
                    <button class="ghost" style="font-size:11px; padding:3px 8px;" @click="copySnippet(clineSnippet)">
                      <iconify-icon icon="mdi:content-copy"></iconify-icon> Copy
                    </button>
                  </div>
                </div>
              </div>

              <!-- Codex CLI -->
              <div class="connect-panel">
                <div class="kube-tutorial-toggle" @click="connectPanel = connectPanel === 'codex' ? '' : 'codex'">
                  <iconify-icon :icon="connectPanel === 'codex' ? 'mdi:chevron-down' : 'mdi:chevron-right'"></iconify-icon>
                  <iconify-icon icon="simple-icons:openai" style="font-size:13px; color:#74aa9c;"></iconify-icon>
                  <span>Codex CLI</span>
                </div>
                <div v-if="connectPanel === 'codex'" class="kube-tutorial">
                  <p>Add to <code>~/.codex/config.toml</code> (global) or <code>.codex/config.toml</code> in your project:</p>
                  <pre>{{ codexSnippet }}</pre>
                  <div class="tutorial-actions">
                    <button class="ghost" style="font-size:11px; padding:3px 8px;" @click="copySnippet(codexSnippet)">
                      <iconify-icon icon="mdi:content-copy"></iconify-icon> Copy
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <div class="toolbar" style="margin-top:20px;">
              <button class="primary" :disabled="isBusy" @click="saveProfile()">Save Profile</button>
              <button class="ghost" :disabled="isBusy || !form.profileName" @click="deleteProfile">Delete Profile</button>
              <div v-if="status.message" class="status" style="margin-left:auto;">
                <span class="status-dot" :class="{ ok: status.state === 'ok', err: status.state === 'error' }"></span>
                <span>{{ status.message }}</span>
              </div>
            </div>
          </div>
        </template>
      </main>

      <!-- Add API Modal -->
      <div v-if="addFlow.open" class="modal-backdrop" @click.self="closeAddFlow">
        <div class="modal-card" style="max-width: 480px; width: 95%;">

          <!-- Step: pick service -->
          <template v-if="addFlow.step === 'pick'">
            <div class="modal-header">
              <iconify-icon icon="mdi:plus-circle-outline" style="font-size:22px; color:var(--blue);"></iconify-icon>
              <span>Add API</span>
            </div>
            <div class="modal-body">
              <p style="color:var(--text-dim); margin:0 0 16px;">Select the type of API to add:</p>
              <div class="service-picker-grid">
                <button class="service-pick-btn" @click="pickService('kubernetes')">
                  <iconify-icon icon="simple-icons:kubernetes" style="font-size:28px; color:#326ce5;"></iconify-icon>
                  <span>Kubernetes</span>
                </button>
                <button class="service-pick-btn" @click="pickService('gitlab')">
                  <iconify-icon icon="simple-icons:gitlab" style="font-size:28px; color:#fc6d26;"></iconify-icon>
                  <span>GitLab</span>
                </button>
                <button class="service-pick-btn" @click="pickService('jira')">
                  <iconify-icon icon="simple-icons:jira" style="font-size:28px; color:#0052cc;"></iconify-icon>
                  <span>Jira</span>
                </button>
                <button class="service-pick-btn" @click="pickService('slack')">
                  <iconify-icon icon="simple-icons:slack" style="font-size:28px; color:#E01E5A;"></iconify-icon>
                  <span>Slack</span>
                </button>
                <button class="service-pick-btn" @click="pickService('gmail')">
                  <iconify-icon icon="simple-icons:gmail" style="font-size:28px; color:#EA4335;"></iconify-icon>
                  <span>Gmail</span>
                </button>
                <button class="service-pick-btn" @click="pickService('library')">
                  <iconify-icon icon="mdi:bookshelf" style="font-size:28px; color:#8B5CF6;"></iconify-icon>
                  <span>Browse Library</span>
                </button>
                <button class="service-pick-btn service-pick-custom" @click="pickService('custom')">
                  <iconify-icon icon="mdi:cloud-search-outline" style="font-size:28px;"></iconify-icon>
                  <span>Custom API</span>
                </button>
              </div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="closeAddFlow">Cancel</button>
            </div>
          </template>

          <!-- Step: kubernetes -->
          <template v-else-if="addFlow.step === 'kubernetes'">
            <div class="modal-header">
              <iconify-icon icon="simple-icons:kubernetes" style="font-size:22px; color:#326ce5;"></iconify-icon>
              <span>Add Kubernetes API</span>
            </div>
            <div class="modal-body">
              <div style="margin-bottom:12px;">
                <label>Cluster API URL</label>
                <input v-model="addFlow.instanceUrl" placeholder="https://localhost:6443" />
              </div>
              <div style="margin-bottom:12px;">
                <label>Service Account Token</label>
                <input v-model="addFlow.token" type="password" placeholder="eyJhbGciOiJSUzI1NiIsImtpZCI6Ii..." />
              </div>
              <div style="margin-bottom:16px;">
                <label>API Name</label>
                <input v-model="addFlow.apiName" placeholder="Kubernetes" />
              </div>

              <!-- Tutorial toggle -->
              <div class="kube-tutorial-toggle" @click="addFlow.kubeTutorial = !addFlow.kubeTutorial">
                <iconify-icon :icon="addFlow.kubeTutorial ? 'mdi:chevron-down' : 'mdi:chevron-right'"></iconify-icon>
                <span>How to generate a service account token</span>
              </div>
              <div v-if="addFlow.kubeTutorial" class="kube-tutorial">
                <p>Run these commands against your cluster:</p>
                <pre># Create a dedicated service account
kubectl create serviceaccount skyline -n default

# Grant cluster-admin (or use a more restrictive role)
kubectl create clusterrolebinding skyline \
  --clusterrole=cluster-admin \
  --serviceaccount=default:skyline

# Generate a long-lived token (1 year)
kubectl create token skyline --duration=8760h</pre>
                <p>Copy the printed token and paste it above.</p>
              </div>

              <div v-if="addFlow.error" class="modal-error">{{ addFlow.error }}</div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="pickService('pick')">Back</button>
              <button class="primary" :disabled="addFlow.busy" @click="addKubernetes">
                <iconify-icon icon="mdi:check"></iconify-icon>
                {{ addFlow.busy ? 'Verifying…' : 'Add to Profile' }}
              </button>
            </div>
          </template>

          <!-- Step: gitlab -->
          <template v-else-if="addFlow.step === 'gitlab'">
            <div class="modal-header">
              <iconify-icon icon="simple-icons:gitlab" style="font-size:22px; color:#fc6d26;"></iconify-icon>
              <span>Add GitLab API</span>
            </div>
            <div class="modal-body">
              <div style="margin-bottom:12px;">
                <label>GitLab Instance URL</label>
                <input v-model="addFlow.instanceUrl" placeholder="https://gitlab.com" />
              </div>
              <div style="margin-bottom:12px;">
                <label>Personal Access Token <span style="color:var(--text-dim); font-size:11px;">(api scope required)</span></label>
                <input v-model="addFlow.token" type="password" placeholder="glpat-xxxxxxxxxxxxxxxxxxxx" />
              </div>
              <div>
                <label>API Name</label>
                <input v-model="addFlow.apiName" placeholder="GitLab" />
              </div>
              <div v-if="addFlow.error" class="modal-error">{{ addFlow.error }}</div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="pickService('pick')">Back</button>
              <button class="primary" :disabled="addFlow.busy" @click="addGitLab">
                <iconify-icon icon="mdi:check"></iconify-icon>
                {{ addFlow.busy ? 'Verifying…' : 'Add to Profile' }}
              </button>
            </div>
          </template>

          <!-- Step: jira -->
          <template v-else-if="addFlow.step === 'jira'">
            <div class="modal-header">
              <iconify-icon icon="simple-icons:jira" style="font-size:22px; color:#0052cc;"></iconify-icon>
              <span>Add Jira API</span>
            </div>
            <div class="modal-body">
              <div style="margin-bottom:12px;">
                <label>Jira Instance URL</label>
                <input v-model="addFlow.instanceUrl" placeholder="https://your-org.atlassian.net" />
              </div>
              <div v-if="addFlow.instanceUrl.includes('.atlassian.net')" style="margin-bottom:12px;">
                <label>Email <span style="color:var(--text-dim); font-size:11px;">(Jira Cloud: used for Basic auth)</span></label>
                <input v-model="addFlow.email" type="email" placeholder="user@example.com" />
              </div>
              <div style="margin-bottom:12px;">
                <label>
                  <span v-if="addFlow.instanceUrl.includes('.atlassian.net')">API Token</span>
                  <span v-else>Personal Access Token</span>
                </label>
                <input v-model="addFlow.token" type="password" placeholder="Your token" />
              </div>
              <div>
                <label>API Name</label>
                <input v-model="addFlow.apiName" placeholder="Jira" />
              </div>
              <div v-if="addFlow.error" class="modal-error">{{ addFlow.error }}</div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="pickService('pick')">Back</button>
              <button class="primary" :disabled="addFlow.busy" @click="addJira">
                <iconify-icon icon="mdi:check"></iconify-icon>
                {{ addFlow.busy ? 'Verifying…' : 'Add to Profile' }}
              </button>
            </div>
          </template>

          <!-- Step: slack -->
          <template v-else-if="addFlow.step === 'slack'">
            <div class="modal-header">
              <iconify-icon icon="simple-icons:slack" style="font-size:22px; color:#E01E5A;"></iconify-icon>
              <span>Add Slack API</span>
            </div>
            <div class="modal-body">
              <div style="margin-bottom:12px;">
                <label>Bot Token or User Token</label>
                <input v-model="addFlow.token" type="password" placeholder="xoxb-... or xoxp-..." />
                <div style="margin-top:6px; font-size:12px; color:var(--text-dim);">
                  <iconify-icon :icon="slackTokenType(addFlow.token) === 'user' ? 'mdi:account' : 'mdi:robot'" style="vertical-align:middle;"></iconify-icon>
                  <span v-if="slackTokenType(addFlow.token) === 'bot'"> Bot token (xoxb-) detected</span>
                  <span v-else-if="slackTokenType(addFlow.token) === 'user'"> User token (xoxp-) detected</span>
                  <span v-else> Enter an xoxb- (bot) or xoxp- (user) token</span>
                </div>
              </div>
              <div>
                <label>API Name</label>
                <input v-model="addFlow.apiName" placeholder="Slack Bot" />
              </div>
              <div v-if="addFlow.error" class="modal-error">{{ addFlow.error }}</div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="pickService('pick')">Back</button>
              <button class="primary" :disabled="addFlow.busy" @click="addSlack">
                <iconify-icon icon="mdi:check"></iconify-icon>
                {{ addFlow.busy ? 'Verifying…' : 'Add to Profile' }}
              </button>
            </div>
          </template>

          <!-- Step: gmail -->
          <template v-else-if="addFlow.step === 'gmail'">
            <div class="modal-header">
              <iconify-icon icon="simple-icons:gmail" style="font-size:22px; color:#EA4335;"></iconify-icon>
              <span>Add Gmail API</span>
            </div>
            <div class="modal-body">
              <div style="margin-bottom:12px;">
                <label>Client ID <span style="color:var(--text-dim); font-size:11px;">(from Google Cloud Console)</span></label>
                <input v-model="addFlow.gmailClientId" placeholder="123456789.apps.googleusercontent.com" />
              </div>
              <div style="margin-bottom:12px;">
                <label>Client Secret</label>
                <input v-model="addFlow.gmailClientSecret" type="password" placeholder="GOCSPX-..." />
              </div>
              <div style="margin-bottom:16px;">
                <label>API Name</label>
                <input v-model="addFlow.apiName" placeholder="Gmail" />
              </div>

              <!-- Tutorial toggle -->
              <div class="kube-tutorial-toggle" @click="addFlow.gmailTutorial = !addFlow.gmailTutorial">
                <iconify-icon :icon="addFlow.gmailTutorial ? 'mdi:chevron-down' : 'mdi:chevron-right'"></iconify-icon>
                <span>How to create Google OAuth credentials</span>
              </div>
              <div v-if="addFlow.gmailTutorial" class="kube-tutorial">
                <p>1. Go to <a href="https://console.cloud.google.com/apis/credentials" target="_blank" style="color:var(--blue);">Google Cloud Console &rarr; APIs &amp; Credentials</a></p>
                <p>2. Create a new project (or select an existing one)</p>
                <p>3. Enable the <strong>Gmail API</strong> in APIs &amp; Services &rarr; Library</p>
                <p>4. Configure the <strong>OAuth consent screen</strong> (External, add your email as test user)</p>
                <p>5. Go to <strong>Credentials</strong> &rarr; Create Credentials &rarr; <strong>OAuth client ID</strong></p>
                <p>6. Application type: <strong>Web application</strong></p>
                <p>7. Add Authorized redirect URI: <code style="background:var(--bg-alt); padding:2px 6px; border-radius:3px;">{{ oauthRedirectHint }}/oauth/callback</code></p>
                <p>8. Copy the Client ID and Client Secret into the fields above</p>
              </div>

              <div v-if="addFlow.error" class="modal-error">{{ addFlow.error }}</div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="pickService('pick')">Back</button>
              <button class="primary" :disabled="addFlow.busy" @click="addGmail">
                <iconify-icon icon="mdi:check"></iconify-icon>
                {{ addFlow.busy ? 'Connecting…' : 'Connect Gmail Account' }}
              </button>
            </div>
          </template>

          <!-- Step: library -->
          <template v-else-if="addFlow.step === 'library'">
            <div class="modal-header">
              <iconify-icon icon="mdi:bookshelf" style="font-size:22px; color:#8B5CF6;"></iconify-icon>
              <span>Import from Library</span>
            </div>
            <div class="modal-body" style="max-height:60vh; overflow-y:auto;">
              <div v-if="addFlow.libraryLoading" style="text-align:center; padding:20px; color:var(--text-dim);">
                Loading library…
              </div>
              <div v-else-if="addFlow.libraryError" style="color:var(--red); font-size:13px;">{{ addFlow.libraryError }}</div>
              <template v-else>
                <div class="primary-input" style="margin-bottom:10px;">
                  <input v-model="addFlow.librarySearch" placeholder="Search APIs..." />
                </div>
                <div style="display:flex; flex-wrap:wrap; gap:4px; margin-bottom:12px;">
                  <button
                    v-for="cat in libraryCategories"
                    :key="cat"
                    style="padding:3px 10px; border-radius:99px; font-size:11px; cursor:pointer; border:1px solid var(--border); transition:all .15s;"
                    :style="addFlow.libraryCategory === cat
                      ? { background: 'var(--blue)', color: '#fff', borderColor: 'var(--blue)' }
                      : { background: 'var(--bg-input)', color: 'var(--text-dim)' }"
                    @click="addFlow.libraryCategory = cat"
                  >{{ cat }}</button>
                </div>
                <div v-if="filteredLibraryItems.length === 0" style="text-align:center; padding:12px; color:var(--text-dim); font-size:13px;">
                  No APIs match your search.
                </div>
                <div
                  v-for="item in filteredLibraryItems"
                  :key="item.id"
                  class="detect-result-item"
                  @click="addFromLibrary(item)"
                  :style="{ opacity: addFlow.busy ? '0.5' : '1', pointerEvents: addFlow.busy ? 'none' : 'auto' }"
                >
                  <img
                    v-if="item.website"
                    :src="'https://www.google.com/s2/favicons?domain=' + new URL(item.website).hostname + '&sz=32'"
                    :alt="item.title"
                    style="width:24px; height:24px; flex-shrink:0; object-fit:contain; border-radius:4px;"
                    @error="$event.target.style.display='none'"
                  />
                  <span
                    v-else
                    style="width:24px; height:24px; flex-shrink:0; display:flex; align-items:center; justify-content:center; border-radius:4px; background:var(--bg-input); color:var(--text-dim); font-size:12px; font-weight:600;"
                  >{{ item.title.trim().charAt(0).toUpperCase() }}</span>
                  <div style="flex:1; min-width:0;">
                    <div style="font-weight:500;">{{ item.title }}</div>
                    <div class="muted" style="font-size:11px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">{{ item.subtitle }}</div>
                  </div>
                  <div style="display:flex; gap:4px; flex-shrink:0;">
                    <span style="font-size:10px; padding:2px 6px; border-radius:99px; background:var(--bg-input); color:var(--text-dim); border:1px solid var(--border);">{{ item.category }}</span>
                  </div>
                  <iconify-icon icon="mdi:plus-circle" style="color:var(--blue); flex-shrink:0;"></iconify-icon>
                </div>
              </template>
              <div v-if="addFlow.error" class="modal-error">{{ addFlow.error }}</div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="pickService('pick')">Back</button>
            </div>
          </template>

          <!-- Step: custom -->
          <template v-else-if="addFlow.step === 'custom'">
            <div class="modal-header">
              <iconify-icon icon="mdi:cloud-search-outline" style="font-size:22px; color:var(--blue);"></iconify-icon>
              <span>Add Custom API</span>
            </div>
            <div class="modal-body">
              <div class="primary-input" style="margin-bottom:12px;">
                <input v-model="addFlow.customUrl" placeholder="https://api.example.com" @keyup.enter="runAddFlowDetect" />
                <button class="primary" :disabled="addFlow.detecting" @click="runAddFlowDetect">
                  <span v-if="addFlow.detecting">Detecting…</span>
                  <span v-else>Detect</span>
                </button>
              </div>
              <div v-if="addFlow.detectError" style="color:var(--red); font-size:13px; margin-bottom:10px;">{{ addFlow.detectError }}</div>
              <div v-if="addFlow.detectResults.length > 0">
                <div style="font-size:12px; color:var(--text-dim); margin-bottom:8px;">Detected APIs — click to add:</div>
                <div
                  v-for="probe in addFlow.detectResults"
                  :key="probe.spec_url"
                  class="detect-result-item"
                  @click="addFromDetectResult(probe)"
                >
                  <iconify-icon :icon="serviceIcons[probe.type] || typeIcons[probe.type] || 'mdi:api'" style="font-size:20px; flex-shrink:0;"></iconify-icon>
                  <div style="flex:1; min-width:0;">
                    <div style="font-weight:500;">{{ typeLabels[probe.type] || probe.type }}</div>
                    <div class="muted" style="font-size:11px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">{{ probe.spec_url }}</div>
                  </div>
                  <iconify-icon icon="mdi:plus-circle" style="color:var(--blue); flex-shrink:0;"></iconify-icon>
                </div>
              </div>
              <div v-if="addFlow.error" class="modal-error">{{ addFlow.error }}</div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="pickService('pick')">Back</button>
            </div>
          </template>

        </div>
      </div>

      <!-- Export Profile Modal -->
      <div v-if="exportFlow.open" class="modal-backdrop" @click.self="closeExportFlow">
        <div class="modal-card" style="max-width:520px; width:95%;">
          <div class="modal-header">
            <iconify-icon icon="mdi:export" style="font-size:22px; color:var(--blue);"></iconify-icon>
            <span>Export Profile — {{ activeProfile }}</span>
          </div>
          <div class="modal-body" style="max-height:60vh; overflow-y:auto;">
            <div style="font-size:12px; color:var(--text-dim); margin-bottom:10px;">Select APIs to include:</div>
            <div v-for="api in form.apis" :key="api.id"
                 style="background:var(--bg-elevated); border:1px solid var(--border); border-radius:var(--radius-sm); padding:10px 12px; margin-bottom:8px;">
              <label style="display:flex; align-items:center; gap:10px; cursor:pointer;">
                <input type="checkbox" v-model="exportFlow.apis[api.id].selected" />
                <iconify-icon :icon="serviceIcons[api.knownService] || typeIcons[api.type] || 'mdi:cloud-outline'" style="font-size:16px; flex-shrink:0;"></iconify-icon>
                <span style="font-weight:500; flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">{{ api.name || api.specUrl }}</span>
              </label>
              <div v-if="exportFlow.apis[api.id].selected"
                   style="display:flex; gap:16px; margin-top:8px; padding-left:26px;">
                <label style="display:flex; align-items:center; gap:6px; font-size:12px; cursor:pointer;"
                       :style="{ opacity: (api.authType && api.authType !== 'none') ? '1' : '0.4' }">
                  <input type="checkbox" v-model="exportFlow.apis[api.id].includeAuth" :disabled="!api.authType || api.authType === 'none'" />
                  Include credentials
                </label>
                <label style="display:flex; align-items:center; gap:6px; font-size:12px; cursor:pointer;"
                       :style="{ opacity: api.filterMode ? '1' : '0.4' }">
                  <input type="checkbox" v-model="exportFlow.apis[api.id].includeFilter" :disabled="!api.filterMode" />
                  Include filter matrix
                </label>
              </div>
            </div>
            <div style="margin-top:14px; border-top:1px solid var(--border); padding-top:14px;">
              <label style="display:flex; align-items:center; gap:8px; cursor:pointer; font-size:13px; font-weight:500;">
                <input type="checkbox" v-model="exportFlow.encrypt" />
                <iconify-icon icon="mdi:lock-outline" style="font-size:16px;"></iconify-icon>
                Encrypt with password
              </label>
              <div v-if="exportFlow.encrypt" style="margin-top:10px; position:relative;">
                <input
                  :type="exportFlow.showPassword ? 'text' : 'password'"
                  v-model="exportFlow.password"
                  placeholder="Encryption password"
                  style="width:100%; padding-right:40px; box-sizing:border-box;"
                />
                <button class="token-toggle" @click="exportFlow.showPassword = !exportFlow.showPassword" type="button" style="position:absolute; right:8px; top:50%; transform:translateY(-50%); background:none; border:none; cursor:pointer; color:var(--text-dim);">
                  <iconify-icon :icon="exportFlow.showPassword ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon>
                </button>
              </div>
            </div>
            <div v-if="exportFlow.error" class="modal-error" style="margin-top:10px;">{{ exportFlow.error }}</div>
          </div>
          <div class="modal-footer">
            <button class="ghost" @click="closeExportFlow">Cancel</button>
            <button class="primary" :disabled="exportFlow.busy" @click="runExport">
              <iconify-icon icon="mdi:download"></iconify-icon>
              {{ exportFlow.busy ? 'Exporting…' : 'Export' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Import Profile Modal -->
      <div v-if="importFlow.open" class="modal-backdrop" @click.self="closeImportFlow">
        <div class="modal-card" style="max-width:540px; width:95%;">

          <!-- Step: pick file -->
          <template v-if="importFlow.step === 'pick'">
            <div class="modal-header">
              <iconify-icon icon="mdi:import" style="font-size:22px; color:var(--blue);"></iconify-icon>
              <span>Import Profile</span>
            </div>
            <div class="modal-body">
              <div style="margin-bottom:14px;">
                <label style="display:block; margin-bottom:6px; font-size:13px;">Import file <span style="color:var(--text-dim);">(.skylineprofile)</span></label>
                <input type="file" accept=".skylineprofile,application/json" @change="handleImportFilePick" style="width:100%; box-sizing:border-box;" />
              </div>
              <div>
                <label style="display:block; margin-bottom:6px; font-size:13px;">Import into</label>
                <select v-model="importFlow.targetProfile" style="width:100%;">
                  <option v-for="name in profiles" :key="name" :value="name">{{ name }}</option>
                  <option value="__new__">+ Create new profile</option>
                </select>
              </div>
              <div v-if="importFlow.targetProfile === '__new__'" style="margin-top:10px;">
                <label style="display:block; margin-bottom:6px; font-size:13px;">New profile name</label>
                <input v-model="importFlow.newProfileName" placeholder="e.g. imported-prod" style="width:100%; box-sizing:border-box;" />
              </div>
              <div v-if="importFlow.error" class="modal-error" style="margin-top:10px;">{{ importFlow.error }}</div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="closeImportFlow">Cancel</button>
            </div>
          </template>

          <!-- Step: password for encrypted file -->
          <template v-else-if="importFlow.step === 'password'">
            <div class="modal-header">
              <iconify-icon icon="mdi:lock-outline" style="font-size:22px; color:var(--blue);"></iconify-icon>
              <span>Enter Decryption Password</span>
            </div>
            <div class="modal-body">
              <p style="color:var(--text-dim); font-size:13px; margin:0 0 12px;">This file is encrypted. Enter the password used during export.</p>
              <div style="position:relative;">
                <input
                  :type="importFlow.showPassword ? 'text' : 'password'"
                  v-model="importFlow.password"
                  placeholder="Decryption password"
                  @keyup.enter="handleImportDecrypt"
                  style="width:100%; padding-right:40px; box-sizing:border-box;"
                  autofocus
                />
                <button @click="importFlow.showPassword = !importFlow.showPassword" type="button" style="position:absolute; right:8px; top:50%; transform:translateY(-50%); background:none; border:none; cursor:pointer; color:var(--text-dim);">
                  <iconify-icon :icon="importFlow.showPassword ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon>
                </button>
              </div>
              <div v-if="importFlow.error" class="modal-error" style="margin-top:10px;">{{ importFlow.error }}</div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="importFlow.step = 'pick'">Back</button>
              <button class="primary" :disabled="importFlow.busy" @click="handleImportDecrypt">
                <iconify-icon icon="mdi:lock-open-outline"></iconify-icon>
                {{ importFlow.busy ? 'Decrypting…' : 'Decrypt' }}
              </button>
            </div>
          </template>

          <!-- Step: select APIs to merge -->
          <template v-else-if="importFlow.step === 'merge'">
            <div class="modal-header">
              <iconify-icon icon="mdi:import" style="font-size:22px; color:var(--blue);"></iconify-icon>
              <span>Select APIs to Import</span>
            </div>
            <div class="modal-body">
              <div style="margin-bottom:12px;">
                <label style="display:block; margin-bottom:6px; font-size:13px;">Import into</label>
                <select v-model="importFlow.targetProfile" @change="onImportTargetChange" style="width:100%;">
                  <option v-for="name in profiles" :key="name" :value="name">{{ name }}</option>
                  <option value="__new__">+ Create new profile</option>
                </select>
                <div v-if="importFlow.targetProfile === '__new__'" style="margin-top:8px;">
                  <input v-model="importFlow.newProfileName" placeholder="New profile name" style="width:100%; box-sizing:border-box;" />
                </div>
              </div>
              <div style="max-height:50vh; overflow-y:auto;">
                <div v-for="row in importFlow.importApis" :key="row.name"
                     style="background:var(--bg-elevated); border:1px solid var(--border); border-radius:var(--radius-sm); padding:10px 12px; margin-bottom:8px;">
                  <div style="display:flex; align-items:center; gap:10px;">
                    <input type="checkbox" v-model="row.selected" />
                    <span style="font-weight:500; flex:1;">{{ row.name }}</span>
                    <span v-if="row.conflicts"
                          style="font-size:11px; color:var(--orange); background:rgba(249,115,22,0.12); padding:2px 7px; border-radius:4px; flex-shrink:0;">
                      will overwrite
                    </span>
                  </div>
                  <div v-if="row.spec_url" style="margin-top:2px; padding-left:26px; font-size:11px; color:var(--text-dim); overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">
                    {{ row.spec_url }}
                  </div>
                  <div v-if="row.selected"
                       style="display:flex; gap:16px; margin-top:8px; padding-left:26px;">
                    <label style="display:flex; align-items:center; gap:6px; font-size:12px; cursor:pointer;"
                           :style="{ opacity: row.hasAuth ? '1' : '0.4' }">
                      <input type="checkbox" v-model="row.importAuth" :disabled="!row.hasAuth" />
                      Import credentials
                    </label>
                    <label style="display:flex; align-items:center; gap:6px; font-size:12px; cursor:pointer;"
                           :style="{ opacity: row.hasFilter ? '1' : '0.4' }">
                      <input type="checkbox" v-model="row.importFilter" :disabled="!row.hasFilter" />
                      Import filter matrix
                    </label>
                  </div>
                </div>
              </div>
              <div v-if="importFlow.error" class="modal-error" style="margin-top:10px;">{{ importFlow.error }}</div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="importFlow.step = importFlow.file?.encrypted ? 'password' : 'pick'">Back</button>
              <button class="primary" :disabled="importFlow.busy" @click="runImport">
                <iconify-icon icon="mdi:check"></iconify-icon>
                {{ importFlow.busy ? 'Importing…' : 'Import' }}
              </button>
            </div>
          </template>

        </div>
      </div>

      <!-- New Profile Modal -->
      <div v-if="showNewProfileModal" class="modal-backdrop" @click.self="closeNewProfileModal">
        <div class="modal-card">
          <div class="modal-header">
            <iconify-icon icon="mdi:folder-plus-outline" style="font-size: 22px; color: var(--blue);"></iconify-icon>
            <span>Create New Profile</span>
          </div>
          <div class="modal-body">
            <label>Profile Name</label>
            <input
              v-model="newProfileName"
              placeholder="e.g. dev, prod, agent-a"
              @keyup.enter="createNewProfile"
              autofocus
            />
            <div v-if="newProfileError" class="modal-error">{{ newProfileError }}</div>
          </div>
          <div class="modal-footer">
            <button class="ghost" @click="closeNewProfileModal">Cancel</button>
            <button class="primary" :disabled="isBusy" @click="createNewProfile">
              <iconify-icon icon="mdi:check"></iconify-icon>
              Create
            </button>
          </div>
        </div>
      </div>
    </div>
  `,
}).mount("#app");

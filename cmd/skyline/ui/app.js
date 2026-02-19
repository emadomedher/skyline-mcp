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
};

// Known service identification — overlays the generic spec-type icon/label
const serviceIcons = {
  kubernetes: "simple-icons:kubernetes",
  gitlab:     "simple-icons:gitlab",
  jira:       "simple-icons:jira",
  slack:      "simple-icons:slack",
};

const serviceLabels = {
  kubernetes: "Kubernetes",
  gitlab:     "GitLab",
  jira:       "Jira",
  slack:      "Slack",
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
  async fetchOperations(specUrl, specType) {
    const res = await fetch("/operations", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ spec_url: specUrl, spec_type: specType }),
    });
    if (!res.ok) {
      const msg = await res.text();
      throw new Error(`Fetch operations failed (${res.status}): ${msg}`);
    }
    return res.json();
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
    // Filter configuration
    filterMode: "",
    filterOperations: [],
    availableOperations: [],
    selectedOperations: new Set(),
    showFilterConfig: false,
    filterLoading: false,
    // Known-service credential helpers
    knownService: "",
    kubeconfigStatus: null,
  };
}

createApp({
  setup() {
    const profiles = ref([]);
    const activeProfile = ref("");
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
      step: "pick", // 'pick' | 'kubernetes' | 'gitlab' | 'jira' | 'slack' | 'custom'
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
    });
    const showToken = ref(false);
    const showNewProfileModal = ref(false);
    const newProfileName = ref("");
    const newProfileError = ref("");
    const selectedApiId = ref("");
    const profileTab = ref("overview");
    const apiTab = ref("stats");
    const expandedProfiles = ref({});
    const profileStats = ref(null);
    const statsLoading = ref(false);
    let isLoadingProfile = false;

    // MCP client connect section
    const connectPanel = ref("");
    const gatewayUrl = computed(() => {
      if (!form.profileName) return "";
      const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
      return `${proto}//${window.location.host}/profiles/${encodeURIComponent(form.profileName)}/gateway`;
    });
    const claudeDesktopSnippet = computed(() => {
      if (!form.profileName || !form.profileToken) return "";
      return JSON.stringify({
        mcpServers: {
          [`skyline-${form.profileName}`]: {
            url: gatewayUrl.value,
            headers: { Authorization: `Bearer ${form.profileToken}` },
          },
        },
      }, null, 2);
    });
    const claudeCodeCmd = computed(() => {
      if (!form.profileName || !form.profileToken) return "";
      return `claude mcp add skyline-${form.profileName} --transport ws ${gatewayUrl.value} --header "Authorization: Bearer ${form.profileToken}"`;
    });
    const claudeCodeSettings = computed(() => {
      if (!form.profileName || !form.profileToken) return "";
      return JSON.stringify({
        mcpServers: {
          [`skyline-${form.profileName}`]: {
            type: "ws",
            url: gatewayUrl.value,
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
            url: gatewayUrl.value,
            headers: { Authorization: `Bearer ${form.profileToken}` },
          },
        },
      }, null, 2);
    });
    const codexSnippet = computed(() => {
      if (!form.profileName || !form.profileToken) return "";
      const key = `skyline-${form.profileName}`;
      return `[mcp_servers.${key}]\nurl = "${gatewayUrl.value}"\nhttp_headers = { Authorization = "Bearer ${form.profileToken}" }`;
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
        // Profile tokens are only needed by MCP servers for gateway access
        const data = await apiClient.loadProfile(name);
        form.profileName = data.name || name;
        form.profileToken = data.token || "";
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
          detectedOnce: true,
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
      try {
        const since = new Date(Date.now() - 24 * 3600 * 1000).toISOString();
        const res = await fetch(`/admin/stats?profile=${encodeURIComponent(name)}&since=${encodeURIComponent(since)}`);
        if (res.ok) {
          const data = await res.json();
          profileStats.value = data.audit_stats || null;
        }
      } catch {
        // stats not critical
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
      apiTab.value = "stats";
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
        await apiClient.saveProfile(form.profileName, form.profileToken, { apis });
        await refreshProfiles();
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

        if (!silent) setStatus("ok", "Profile saved.");
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
      if (!api.specUrl) {
        setStatus("error", "Spec URL is required to fetch operations.");
        return;
      }
      try {
        api.filterLoading = true;
        const result = await apiClient.fetchOperations(api.specUrl, api.type);
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
      setStatus("ok", "Filter cleared.");
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
      });
    }

    function closeAddFlow() {
      addFlow.open = false;
    }

    function pickService(svc) {
      addFlow.step = svc;
      addFlow.error = "";
      if (svc === "gitlab" && !addFlow.instanceUrl) addFlow.instanceUrl = "https://gitlab.com";
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

    onMounted(refreshProfiles);

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
      runAddFlowDetect,
      addFromDetectResult,
      // Profile tree / tabs
      selectedApiId,
      selectedApi,
      profileTab,
      apiTab,
      expandedProfiles,
      profileStats,
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
    };
  },
  template: `
    <div class="app">
      <aside class="panel">
        <div class="profile-list">
          <div class="sidebar-header">
            <span class="notice" style="margin:0; padding:0;">Profiles</span>
            <button class="icon-btn" @click="openNewProfileModal" title="New Profile">
              <iconify-icon icon="mdi:plus-circle-outline"></iconify-icon>
            </button>
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
          <div class="tab-bar">
            <button :class="['tab-btn', { active: apiTab === 'stats' }]" @click="apiTab = 'stats'">Stats</button>
            <button :class="['tab-btn', { active: apiTab === 'config' }]" @click="apiTab = 'config'">Config</button>
          </div>

          <div v-if="apiTab === 'stats'" class="tab-content">
            <div class="welcome-screen" style="min-height:200px;">
              <div class="welcome-content">
                <iconify-icon icon="mdi:chart-bar" style="font-size:40px; color:var(--text-dim);"></iconify-icon>
                <p style="color:var(--text-dim); margin-top:12px;">Per-API stats coming soon.</p>
              </div>
            </div>
          </div>

          <div v-else-if="apiTab === 'config'" class="tab-content">
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
                  <label>Detected spec URL</label>
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
                <div><label>Bearer token</label><input v-model="selectedApi.bearerToken" type="password" /></div>
              </div>
              <div v-if="selectedApi.authType === 'basic'" class="form-grid">
                <div><label>Username</label><input v-model="selectedApi.basicUser" /></div>
                <div><label>Password</label><input v-model="selectedApi.basicPass" type="password" /></div>
              </div>
              <div v-if="selectedApi.authType === 'api-key'" class="form-grid">
                <div><label>Header</label><input v-model="selectedApi.apiKeyHeader" /></div>
                <div><label>Value</label><input v-model="selectedApi.apiKeyValue" type="password" /></div>
              </div>

              <!-- Known-service credential helpers -->
              <div v-if="selectedApi.knownService === 'gitlab'" class="credential-helper gitlab-helper">
                <div class="helper-header"><iconify-icon icon="simple-icons:gitlab"></iconify-icon>GitLab Authentication</div>
                <p class="helper-desc">Personal Access Token (requires <code>api</code> scope).</p>
                <input type="password" placeholder="glpat-xxxxxxxxxxxxxxxxxxxx" :value="selectedApi.bearerToken"
                  @input="selectedApi.authType='bearer'; selectedApi.bearerToken=$event.target.value" />
              </div>
              <div v-if="selectedApi.knownService === 'jira'" class="credential-helper jira-helper">
                <div class="helper-header"><iconify-icon icon="simple-icons:jira"></iconify-icon>Jira Authentication</div>
                <p class="helper-desc">Personal Access Token.</p>
                <input type="password" placeholder="Your Jira PAT" :value="selectedApi.bearerToken"
                  @input="selectedApi.authType='bearer'; selectedApi.bearerToken=$event.target.value" />
              </div>
              <div v-if="selectedApi.knownService === 'slack'" class="credential-helper slack-helper">
                <div class="helper-header"><iconify-icon icon="simple-icons:slack"></iconify-icon>Slack Authentication</div>
                <p class="helper-desc">
                  <span v-if="slackTokenType(selectedApi.bearerToken) === 'bot'">Bot token detected</span>
                  <span v-else-if="slackTokenType(selectedApi.bearerToken) === 'user'">User token detected</span>
                  <span v-else>Enter an <code>xoxb-</code> Bot Token or <code>xoxp-</code> User Token</span>
                </p>
                <input type="password" placeholder="xoxb-... or xoxp-..." :value="selectedApi.bearerToken"
                  @input="selectedApi.authType='bearer'; selectedApi.bearerToken=$event.target.value" />
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
                    <div class="operations-header">
                      <div class="operations-counter">
                        <iconify-icon icon="mdi:format-list-checks"></iconify-icon>
                        Select Operations
                        <span class="counter-badge">{{ selectedApi.selectedOperations.size }} / {{ selectedApi.availableOperations.length }}</span>
                      </div>
                    </div>
                    <div class="operations-list">
                      <div
                        v-for="op in selectedApi.availableOperations"
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

          <!-- Overview tab: stats -->
          <div v-if="profileTab === 'overview'" class="tab-content">
            <div v-if="statsLoading" class="loading-state" style="padding:40px; text-align:center;">
              <iconify-icon icon="mdi:loading" style="font-size:32px; color:var(--text-dim);"></iconify-icon>
              <div style="margin-top:12px; color:var(--text-dim);">Loading stats...</div>
            </div>
            <div v-else-if="!profileStats || profileStats.total_requests === 0" class="welcome-screen" style="min-height:200px;">
              <div class="welcome-content">
                <iconify-icon icon="mdi:chart-bar" style="font-size:40px; color:var(--text-dim);"></iconify-icon>
                <p style="color:var(--text-dim); margin-top:12px;">No activity in the last 24 hours.</p>
                <p style="color:var(--text-dim); font-size:12px;">Connect a client to start seeing usage data here.</p>
              </div>
            </div>
            <div v-else>
              <div class="stats-grid">
                <div class="stat-card">
                  <div class="stat-value">{{ profileStats.total_requests }}</div>
                  <div class="stat-label">Total Calls (24h)</div>
                </div>
                <div class="stat-card" :class="{ 'stat-error': profileStats.error_rate > 10 }">
                  <div class="stat-value">{{ profileStats.error_rate.toFixed(1) }}%</div>
                  <div class="stat-label">Error Rate</div>
                </div>
                <div class="stat-card">
                  <div class="stat-value">{{ profileStats.avg_duration_ms }}ms</div>
                  <div class="stat-label">Avg Duration</div>
                </div>
              </div>

              <div v-if="profileStats.top_apis && profileStats.top_apis.length > 0" class="stats-section">
                <div class="stats-section-title">Top APIs</div>
                <div class="stats-table">
                  <div class="stats-row header"><span>API</span><span>Calls</span><span>Errors</span><span>Avg ms</span></div>
                  <div v-for="a in profileStats.top_apis" :key="a.name" class="stats-row">
                    <span>{{ a.name }}</span><span>{{ a.calls }}</span><span>{{ a.errors }}</span><span>{{ a.avg_ms }}</span>
                  </div>
                </div>
              </div>

              <div v-if="profileStats.top_tools && profileStats.top_tools.length > 0" class="stats-section">
                <div class="stats-section-title">Top Tools</div>
                <div class="stats-table">
                  <div class="stats-row header"><span>Tool</span><span>Calls</span><span>Errors</span><span>Avg ms</span></div>
                  <div v-for="t in profileStats.top_tools" :key="t.name" class="stats-row">
                    <span>{{ t.name }}</span><span>{{ t.calls }}</span><span>{{ t.errors }}</span><span>{{ t.avg_ms }}</span>
                  </div>
                </div>
              </div>

              <div v-if="profileStats.recent_events && profileStats.recent_events.length > 0" class="stats-section">
                <div class="stats-section-title">Recent Activity</div>
                <div v-for="e in profileStats.recent_events.slice(0, 10)" :key="e.id" class="event-row">
                  <span class="event-dot" :class="{ ok: e.success, err: !e.success }"></span>
                  <span class="event-tool">{{ e.tool_name || e.event_type }}</span>
                  <span class="event-api muted">{{ e.api_name }}</span>
                  <span class="event-dur muted">{{ e.duration_ms }}ms</span>
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

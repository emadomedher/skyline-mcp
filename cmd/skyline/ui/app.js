import { createApp, onMounted, reactive, ref } from "https://unpkg.com/vue@3/dist/vue.esm-browser.js";

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
  async detect(baseUrl) {
    const res = await fetch("/detect", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ base_url: baseUrl }),
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
    });
    const status = reactive({ state: "idle", message: "" });
    const isBusy = ref(false);
    const addedToast = ref(false);
    const showToken = ref(false);

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
            const apiTypes = new Set(apis.map(api => inferType(api.spec_url || "")).filter(Boolean));
            profileMetadata.value[name] = {
              apiCount: apis.length,
              types: Array.from(apiTypes)
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
        const data = await apiClient.detect(api.baseUrl);
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
        if (!api.name) {
          const host = domainFromBaseURL(api.baseUrl);
          const label = typeLabels[api.type] || api.type;
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
        if (!draft.name) {
          const host = domainFromBaseURL(draft.baseUrl);
          const label = typeLabels[draft.type] || draft.type;
          draft.name = host ? `${host} - ${label}` : `${label}`;
        }
        draft.detectedOnce = true;
        draft.status = `Detected ${typeLabels[draft.type] || draft.type}`;
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
        }));

        // Extract and store profile metadata for sidebar display
        const apiTypes = new Set(form.apis.map(api => api.type).filter(Boolean));
        profileMetadata.value[name] = {
          apiCount: form.apis.length,
          types: Array.from(apiTypes)
        };

        setStatus("ok", "Profile loaded.");
      } catch (err) {
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

    async function saveProfile() {
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
        const apiTypes = new Set(form.apis.map(api => api.type).filter(Boolean));
        profileMetadata.value[form.profileName] = {
          apiCount: form.apis.length,
          types: Array.from(apiTypes)
        };

        setStatus("ok", "Profile saved.");
      } catch (err) {
        setStatus("error", err.message);
      } finally {
        isBusy.value = false;
      }
    }

    function newProfile() {
      form.profileName = "";
      form.profileToken = "";
      form.apis = [];
      activeProfile.value = "";
      setStatus("ok", "Ready to create new profile");
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
      newProfile,
      deleteProfile,
      addedToast,
      toggleFilterConfig,
      fetchOperations,
      toggleOperationSelection,
      onFilterModeChange,
      clearFilter,
      showToken,
      toggleTokenVisibility,
    };
  },
  template: `
    <div class="app">
      <aside class="panel">
        <div class="hero">
          <div class="brand">
            <img src="/ui/skyline-logo.svg" alt="Skyline" style="max-width: 180px; height: auto; margin-bottom: 12px;">
          </div>
          <p class="subtitle" style="margin-top: 8px;">Centralized API Gateway for MCP Servers</p>
          <div class="toolbar">
            <button class="primary" @click="refreshProfiles">Refresh</button>
          </div>
        </div>

        <div class="profile-list">
          <div class="notice">Profiles</div>
          <button class="profile-item new-profile-btn" @click="newProfile" style="width: 100%; text-align: left; border: 1px dashed var(--blue); background: transparent; color: var(--blue); cursor: pointer; margin-bottom: 8px;">
            <iconify-icon icon="mdi:plus-circle-outline" style="margin-right: 8px;"></iconify-icon>
            New Profile
          </button>
          <div
            v-for="name in profiles"
            :key="name"
            class="profile-item"
            :class="{ active: name === activeProfile }"
            @click="loadProfile(name)"
          >
            <div class="profile-item-content">
              <div class="profile-item-header">
                <span class="profile-name">{{ name }}</span>
                <span v-if="profileMetadata[name]" class="profile-api-count">
                  {{ profileMetadata[name].apiCount }} API{{ profileMetadata[name].apiCount !== 1 ? 's' : '' }}
                </span>
              </div>
              <div v-if="profileMetadata[name] && profileMetadata[name].types.length > 0" class="profile-types">
                <iconify-icon
                  v-for="type in profileMetadata[name].types"
                  :key="type"
                  :icon="typeIcons[type] || 'mdi:api'"
                  :title="typeLabels[type] || type"
                  class="profile-type-icon"
                ></iconify-icon>
              </div>
            </div>
          </div>
          <div v-if="profiles.length === 0" class="notice">No profiles yet.</div>
          <div v-if="profiles.length > 0 && !activeProfile" class="notice" style="margin-top: 12px; font-size: 12px; opacity: 0.7;">
            💡 Click a profile to load and edit it
          </div>
        </div>
      </aside>

      <main class="panel">
        <!-- Profile Configuration at Top -->
        <div class="profile-header-card">
          <div class="profile-header-title">
            <iconify-icon icon="mdi:folder-account"></iconify-icon>
            Profile Configuration
          </div>
          <div class="form-grid" style="margin-top: 16px;">
            <div>
              <label>Profile name</label>
              <input v-model="form.profileName" placeholder="dev, prod, agent-a" />
            </div>
            <div>
              <label>Profile token (cryptographically secure)</label>
              <div v-if="form.profileToken" style="position: relative;">
                <input
                  :value="form.profileToken"
                  :type="showToken ? 'text' : 'password'"
                  disabled
                  style="width: 100%; padding-right: 40px;"
                />
                <button
                  class="token-toggle"
                  @click="toggleTokenVisibility"
                  type="button"
                  title="Toggle token visibility"
                >
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

        <!-- Add API Section -->
        <div class="hero-card" style="margin-top: 18px;">
          <div class="api-header">
            <div class="step-pill">Add API</div>
            <div class="status">
              <span class="status-dot" :class="{ ok: status.state === 'ok', err: status.state === 'error' }"></span>
              <span>{{ status.message || "Ready" }}</span>
            </div>
          </div>
          <div class="primary-input" style="margin-top:12px;">
            <input v-model="draft.baseUrl" placeholder="https://api.example.com" @blur="detectDraftOnBlur" />
            <button class="primary" :disabled="isBusy" @click="detectDraft">Detect</button>
            <button class="secondary" :disabled="isBusy || !draft.detectedOnce" @click="addDraftToList">Add</button>
          </div>
          <div v-if="addedToast" class="toast fade-in" style="margin-top:10px;">Added to profile</div>

          <div v-if="draft.detectedOnce" class="api-card" style="margin-top:12px;">
            <div class="api-header">
              <div class="api-type">
                <iconify-icon :icon="typeIcons[draft.type] || 'mdi:cloud-outline'"></iconify-icon>
                <div>
                  <div>{{ draft.type ? typeLabels[draft.type] : "Unknown type" }}</div>
                  <div class="muted">{{ draft.status || "Detected" }}</div>
                </div>
              </div>
              <div class="api-actions">
              </div>
            </div>

            <div class="form-grid">
              <div>
                <label>Base URL</label>
                <input v-model="draft.baseUrl" placeholder="http://localhost:9999" />
              </div>
              <div>
                <label>API name</label>
                <input v-model="draft.name" placeholder="domain - API type" />
              </div>
            </div>

            <div class="form-grid">
              <div>
                <label>Detected spec URL</label>
                <input v-model="draft.specUrl" placeholder="autofilled after detect" />
              </div>
              <div>
                <label>Auth type</label>
                <select v-model="draft.authType">
                  <option value="none">None</option>
                  <option value="bearer">Bearer</option>
                  <option value="basic">Basic</option>
                  <option value="api-key">API Key</option>
                </select>
              </div>
            </div>

            <div v-if="draft.detectedOptions.length > 1" class="form-grid">
              <div>
                <label>Detected types</label>
                <select @change="selectDetectedOption(draft, draft.detectedOptions[$event.target.selectedIndex])">
                  <option v-for="opt in draft.detectedOptions" :key="opt.spec_url">
                    {{ typeLabels[opt.type] || opt.type }} — {{ opt.spec_url }}
                  </option>
                </select>
              </div>
            </div>

            <div v-if="draft.authType === 'bearer'" class="form-grid">
              <div>
                <label>Bearer token</label>
                <input v-model="draft.bearerToken" type="password" />
              </div>
            </div>
            <div v-if="draft.authType === 'basic'" class="form-grid">
              <div>
                <label>Username</label>
                <input v-model="draft.basicUser" />
              </div>
              <div>
                <label>Password</label>
                <input v-model="draft.basicPass" type="password" />
              </div>
            </div>
            <div v-if="draft.authType === 'api-key'" class="form-grid">
              <div>
                <label>Header</label>
                <input v-model="draft.apiKeyHeader" />
              </div>
              <div>
                <label>Value</label>
                <input v-model="draft.apiKeyValue" type="password" />
              </div>
            </div>
          </div>
        </div>

        <!-- API List Section -->
        <div class="details-panel" style="margin-top:18px;">
          <div class="step-pill" style="margin-bottom: 12px;">APIs in Profile</div>

          <div v-if="form.apis.length === 0" style="text-align: center; padding: 40px 20px; color: var(--text-dim); opacity: 0.6;">
            No APIs added yet
          </div>

          <div v-for="api in form.apis" :key="api.id" class="api-card">
            <div class="api-header">
              <div class="api-type">
                <iconify-icon :icon="typeIcons[api.type] || 'mdi:cloud-outline'"></iconify-icon>
                <div>
                  <div>{{ api.type ? typeLabels[api.type] : "Unknown type" }}</div>
                  <div class="muted">{{ api.name || api.specUrl }}</div>
                </div>
              </div>
              <div class="api-actions">
                <button class="secondary" :disabled="isBusy" @click="detectApi(api)">Re-detect</button>
                <button class="secondary" :disabled="isBusy" @click="testApi(api)">Test</button>
                <button class="ghost" @click="removeApi(api.id)">Remove</button>
              </div>
            </div>

            <div class="form-grid">
              <div>
                <label>Base URL</label>
                <input v-model="api.baseUrl" placeholder="http://localhost:9999" @blur="detectOnBlur(api)" />
              </div>
              <div>
                <label>API name</label>
                <input v-model="api.name" placeholder="domain - API type" />
              </div>
            </div>

            <div class="form-grid">
              <div>
                <label>Detected spec URL</label>
                <input v-model="api.specUrl" placeholder="autofilled after detect" />
              </div>
              <div>
                <label>Auth type</label>
                <select v-model="api.authType">
                  <option value="none">None</option>
                  <option value="bearer">Bearer</option>
                  <option value="basic">Basic</option>
                  <option value="api-key">API Key</option>
                </select>
              </div>
            </div>

            <div v-if="api.detectedOptions.length > 1" class="form-grid">
              <div>
                <label>Detected types</label>
                <select @change="selectDetectedOption(api, api.detectedOptions[$event.target.selectedIndex])">
                  <option v-for="opt in api.detectedOptions" :key="opt.spec_url">
                    {{ typeLabels[opt.type] || opt.type }} — {{ opt.spec_url }}
                  </option>
                </select>
              </div>
            </div>

            <div v-if="api.authType === 'bearer'" class="form-grid">
              <div>
                <label>Bearer token</label>
                <input v-model="api.bearerToken" type="password" />
              </div>
            </div>
            <div v-if="api.authType === 'basic'" class="form-grid">
              <div>
                <label>Username</label>
                <input v-model="api.basicUser" />
              </div>
              <div>
                <label>Password</label>
                <input v-model="api.basicPass" type="password" />
              </div>
            </div>
            <div v-if="api.authType === 'api-key'" class="form-grid">
              <div>
                <label>Header</label>
                <input v-model="api.apiKeyHeader" />
              </div>
              <div>
                <label>Value</label>
                <input v-model="api.apiKeyValue" type="password" />
              </div>
            </div>

            <!-- Filter Configuration Section -->
            <div class="filter-section">
              <div class="filter-header">
                <div class="filter-info">
                  <div class="filter-title">
                    <iconify-icon icon="mdi:filter-variant"></iconify-icon>
                    Operation Filter
                  </div>
                  <div class="filter-status" :class="{ active: api.filterMode }">
                    <span v-if="!api.filterMode">No filter configured — all operations allowed</span>
                    <span v-else>
                      <strong>{{ api.filterMode === 'allowlist' ? 'Allowlist' : 'Blocklist' }}</strong> mode active
                      · {{ api.filterOperations.length }} pattern{{ api.filterOperations.length !== 1 ? 's' : '' }}
                    </span>
                  </div>
                </div>
                <div class="filter-actions">
                  <button class="secondary" @click="toggleFilterConfig(api)">
                    <iconify-icon :icon="api.showFilterConfig ? 'mdi:chevron-up' : 'mdi:tune'"></iconify-icon>
                    {{ api.showFilterConfig ? 'Hide' : 'Configure' }}
                  </button>
                  <button v-if="api.filterMode" class="ghost" @click="clearFilter(api)">
                    <iconify-icon icon="mdi:close"></iconify-icon>
                    Clear
                  </button>
                </div>
              </div>

              <!-- Filter Configuration Panel -->
              <div v-if="api.showFilterConfig" class="filter-config-panel">
                <div class="filter-mode-selector">
                  <label>
                    <iconify-icon icon="mdi:shield-check"></iconify-icon>
                    Filter Mode
                  </label>
                  <select v-model="api.filterMode" @change="onFilterModeChange(api)">
                    <option value="">Choose filter strategy...</option>
                    <option value="allowlist">✓ Allowlist — Only selected operations allowed</option>
                    <option value="blocklist">✗ Blocklist — Selected operations blocked</option>
                  </select>
                </div>

                <!-- Loading State -->
                <div v-if="api.filterLoading" class="loading-state">
                  <iconify-icon icon="mdi:loading"></iconify-icon>
                  <div style="margin-top: 12px;">Loading operations from API spec...</div>
                </div>

                <!-- Operations List -->
                <div v-else-if="api.availableOperations.length > 0">
                  <div class="operations-header">
                    <div class="operations-counter">
                      <iconify-icon icon="mdi:format-list-checks"></iconify-icon>
                      Select Operations
                      <span class="counter-badge">{{ api.selectedOperations.size }} / {{ api.availableOperations.length }}</span>
                    </div>
                  </div>

                  <div class="operations-list">
                    <div
                      v-for="op in api.availableOperations"
                      :key="op.id"
                      class="operation-item"
                      :class="{ selected: api.selectedOperations.has(op.id) }"
                      @click="toggleOperationSelection(api, op.id)"
                    >
                      <input
                        type="checkbox"
                        class="operation-checkbox"
                        :checked="api.selectedOperations.has(op.id)"
                        @click.stop="toggleOperationSelection(api, op.id)"
                      />
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

                  <!-- Live Preview -->
                  <div class="filter-live-preview" v-if="api.filterMode && api.selectedOperations.size > 0">
                    <iconify-icon icon="mdi:check-circle"></iconify-icon>
                    <span v-if="api.filterMode === 'allowlist'">
                      Exposing <strong>{{ api.selectedOperations.size }}</strong> of {{ api.availableOperations.length }} operations
                    </span>
                    <span v-else>
                      Blocking <strong>{{ api.selectedOperations.size }}</strong>, exposing {{ api.availableOperations.length - api.selectedOperations.size }}
                    </span>
                  </div>
                </div>

                <!-- Empty State -->
                <div v-else class="empty-state">
                  <iconify-icon icon="mdi:file-document-outline"></iconify-icon>
                  <div style="margin-top: 8px; font-weight: 500;">No operations loaded yet</div>
                  <div style="margin-top: 4px; font-size: 12px;">Operations will be fetched automatically from your API spec</div>
                </div>
              </div>
            </div>
          </div>

        </div>

        <div class="toolbar" style="margin-top:20px;">
          <button class="primary" :disabled="isBusy" @click="saveProfile">Save Profile</button>
          <button class="ghost" :disabled="isBusy || !form.profileName" @click="deleteProfile">Delete Profile</button>
        </div>
      </main>
    </div>
  `,
}).mount("#app");

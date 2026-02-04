import { createApp, onMounted, reactive, ref } from "https://unpkg.com/vue@3/dist/vue.esm-browser.js";

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
};

function blankApi() {
  return {
    id: crypto.randomUUID(),
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
  };
}

createApp({
  setup() {
    const profiles = ref([]);
    const activeProfile = ref("");
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

    async function refreshProfiles() {
      try {
        const data = await apiClient.listProfiles();
        profiles.value = data.profiles || [];
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
      if (!form.profileToken) {
        setStatus("error", "Profile token required to load.");
        return;
      }
      try {
        isBusy.value = true;
        const data = await apiClient.loadProfile(name, form.profileToken);
        form.profileName = data.name || name;
        activeProfile.value = name;
        const cfg = data.config || {};
        form.apis = (cfg.apis || []).map((api) => ({
          id: crypto.randomUUID(),
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
        }));
        setStatus("ok", "Profile loaded.");
      } catch (err) {
        setStatus("error", err.message);
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
      if (!form.profileToken) {
        setStatus("error", "Profile token required.");
        return;
      }
      const apis = form.apis
        .filter((api) => api.name && api.specUrl)
        .map((api) => {
          const entry = {
            name: api.name,
            spec_url: api.specUrl,
            base_url_override: api.baseUrl || undefined,
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
          return entry;
        });
      if (apis.length === 0) {
        setStatus("error", "Add at least one API with a detected spec.");
        return;
      }
      try {
        isBusy.value = true;
        await apiClient.saveProfile(form.profileName, form.profileToken, { apis });
        await refreshProfiles();
        activeProfile.value = form.profileName;
        setStatus("ok", "Profile saved.");
      } catch (err) {
        setStatus("error", err.message);
      } finally {
        isBusy.value = false;
      }
    }

    async function deleteProfile() {
      if (!form.profileName) return;
      if (!confirm(`Delete profile "${form.profileName}"?`)) return;
      try {
        isBusy.value = true;
        await apiClient.deleteProfile(form.profileName, form.profileToken);
        await refreshProfiles();
        form.profileName = "";
        form.apis = [];
        activeProfile.value = "";
        setStatus("ok", "Profile deleted.");
      } catch (err) {
        setStatus("error", err.message);
      } finally {
        isBusy.value = false;
      }
    }

    onMounted(refreshProfiles);

    return {
      profiles,
      activeProfile,
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
      deleteProfile,
      addedToast,
    };
  },
  template: `
    <div class="skyline"></div>
    <div class="app">
      <aside class="panel">
        <div class="hero">
          <div class="brand">
            <div class="brand-mark">SKY</div>
            <div>
              <h2>Skyline MCP</h2>
              <p class="subtitle">Profiles that hide secrets from agents.</p>
            </div>
          </div>
          <div class="toolbar">
            <button class="primary" @click="refreshProfiles">Refresh</button>
          </div>
        </div>

        <div class="profile-list">
          <div class="notice">Profiles</div>
          <div
            v-for="name in profiles"
            :key="name"
            class="profile-item"
            :class="{ active: name === activeProfile }"
            @click="loadProfile(name)"
          >
            <span>{{ name }}</span>
            <span class="chip">profile</span>
          </div>
          <div v-if="profiles.length === 0" class="notice">No profiles yet.</div>
        </div>
      </aside>

      <main class="panel">
        <div class="hero-card">
          <div class="api-header">
            <div>
              <div class="step-pill">Step 1 · Add API URL</div>
              <div class="hero-title">Start with the API URL.</div>
              <div class="hero-subtitle">We’ll detect the type automatically and unlock the details.</div>
            </div>
            <div class="status">
              <span class="status-dot" :class="{ ok: status.state === 'ok', err: status.state === 'error' }"></span>
              <span>{{ status.message || "Ready." }}</span>
            </div>
          </div>
          <div class="primary-input" style="margin-top:16px;">
            <input v-model="draft.baseUrl" placeholder="https://api.example.com" @blur="detectDraftOnBlur" />
            <button class="primary" :disabled="isBusy" @click="detectDraft">Detect</button>
            <button class="secondary" :disabled="isBusy || !draft.detectedOnce" @click="addDraftToList">Add</button>
          </div>
          <div class="notice" :class="{ ok: status.state === 'ok', err: status.state === 'error' }" style="margin-top:10px;">
            {{ draft.status || "Enter a URL and click Detect." }}
          </div>
          <div v-if="addedToast" class="toast fade-in" style="margin-top:10px;">Added to profile</div>

          <div v-if="!draft.detectedOnce" class="muted-card" style="margin-top:12px;">
            Detection will populate type + spec URL. You can add auth and metadata after that.
          </div>

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

        <div class="details-panel" style="margin-top:18px;">
          <div class="form-grid">
            <div>
              <label>Profile name</label>
              <input v-model="form.profileName" placeholder="dev, prod, agent-a" />
            </div>
            <div>
              <label>Profile token</label>
              <input v-model="form.profileToken" type="password" placeholder="per-profile bearer token" />
            </div>
          </div>

          <div v-if="form.apis.length > 0" class="step-pill">Added APIs</div>

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

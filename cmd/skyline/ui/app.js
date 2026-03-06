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
  email: "mdi:email-outline",
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
  email: "Email (SMTP/IMAP)",
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

function faviconUrl(website) {
  try { return 'https://icons.duckduckgo.com/ip3/' + new URL(website).hostname + '.ico'; }
  catch { return ''; }
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
    // Rate limiting (per-API)
    rateLimitRpm: "",
    rateLimitRph: "",
    rateLimitRpd: "",
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
    // Email protocol (spec_type: "email")
    emailAddress: "",
    emailPassword: "",
    emailSmtpHost: "",
    emailSmtpPort: "",
    emailSmtpTls: "starttls",
    emailImapHost: "",
    emailImapPort: "",
    emailPop3Host: "",
    emailPop3Port: "",
    emailConnectionMode: "basic",
    emailProvider: "",
    // UI state for inline expansion
    showAdvanced: false,
  };
}

createApp({
  setup() {
    const profiles = ref([]);
    const activeProfile = ref("");
    const defaultProfile = ref("default"); // Name of the undeletable default profile
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
      step: "pick", // 'pick' | 'custom' | 'library' | 'guided'
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
    // Library URL (shared by inline search and modal)
    const LIBRARY_URL = "https://raw.githubusercontent.com/emadomedher/skyline-api-library/main/profiles-slim.json";

    // Built-in protocols — always available, even when library cannot be fetched.
    const builtinProfiles = [
      {
        id: 'email-generic', title: 'Email (Generic)',
        subtitle: 'Send and read email via SMTP/IMAP with any provider',
        category: 'Communication', authType: 'none',
        specUrl: '', specType: 'email', baseUrl: '',
        website: 'https://skylinemcp.com',
        setup: {
          fields: [
            { key: 'email', label: 'Email Address', type: 'text', placeholder: 'you@example.com', required: true },
            { key: 'password', label: 'Password / App Password', type: 'password', placeholder: 'Your email password or app-specific password', required: true },
            { key: 'name', label: 'API Name', type: 'text', placeholder: 'email', default: 'email', target: 'name' },
          ],
          tutorial: '1. Enter your email address and password\n2. For Gmail: Go to myaccount.google.com/apppasswords and generate an App Password\n3. For Outlook: Enable IMAP in Settings > Mail > Sync email\n4. For Yahoo: Generate an App Password in Account Security settings\n5. Skyline auto-detects your provider and fills in server settings\n6. For custom/corporate email, you may need to enter server details manually',
        },
        _builtin: true,
      },
      {
        id: 'custom-api', title: 'Custom API',
        subtitle: 'Paste a URL and auto-detect any OpenAPI/REST/GraphQL endpoint',
        category: 'Custom', authType: 'none',
        specUrl: '', specType: '', baseUrl: '',
        website: '',
        _builtin: true,
        _custom: true,
      },
    ];

    // Persistent library cache (survives addFlow resets)
    const libraryCache = ref([...builtinProfiles]);
    const libraryLoaded = ref(false);
    const libraryLoading = ref(false);
    const libraryLoadError = ref("");

    // Inline library search (for empty-state APIs tab)
    const inlineSearch = ref("");
    const inlineSearchFocused = ref(false);
    function blurInlineSearch() { window.setTimeout(() => { inlineSearchFocused.value = false; }, 200); }

    const popularApiIds = ['email-generic', 'custom-api', 'slack', 'github', 'stripe', 'jira', 'gitlab', 'kubernetes', 'gmail', 'notion', 'discord'];

    async function ensureLibraryLoaded() {
      if (libraryLoaded.value || libraryLoading.value) return;
      libraryLoading.value = true;
      libraryLoadError.value = "";
      try {
        const res = await fetch(LIBRARY_URL);
        if (!res.ok) throw new Error(`Failed (${res.status})`);
        const data = await res.json();
        const remote = (data.profiles || []).map((p) => ({
          id: p.id, title: p.t, subtitle: p.d || "",
          category: p.c, authType: p.at,
          specUrl: p.su || "", specType: p.st || "",
          baseUrl: p.bu || "", website: p.w || "",
          setup: p.s || null,
        }));
        // Merge: built-in profiles replace any remote duplicates
        const builtinIds = new Set(builtinProfiles.map(b => b.id));
        libraryCache.value = [
          ...builtinProfiles,
          ...remote.filter(p => !builtinIds.has(p.id)),
        ];
        libraryLoaded.value = true;
      } catch (err) {
        libraryLoadError.value = err.message;
        // Built-ins are still in libraryCache even on failure
      } finally {
        libraryLoading.value = false;
      }
    }

    const popularApis = computed(() => {
      return popularApiIds
        .map(id => libraryCache.value.find(p => p.id === id))
        .filter(Boolean);
    });

    const inlineSearchResults = computed(() => {
      const q = inlineSearch.value.toLowerCase().trim();
      if (!q || libraryCache.value.length === 0) return [];
      const matches = libraryCache.value
        .filter(p => p.title.toLowerCase().includes(q) || p.subtitle.toLowerCase().includes(q));
      // Rank: exact title match first, then title starts-with, then title contains, then subtitle-only
      matches.sort((a, b) => {
        const at = a.title.toLowerCase(), bt = b.title.toLowerCase();
        const aExact = at === q, bExact = bt === q;
        if (aExact !== bExact) return aExact ? -1 : 1;
        const aPrefix = at.startsWith(q), bPrefix = bt.startsWith(q);
        if (aPrefix !== bPrefix) return aPrefix ? -1 : 1;
        const aTitle = at.includes(q), bTitle = bt.includes(q);
        if (aTitle !== bTitle) return aTitle ? -1 : 1;
        return 0;
      });
      return matches.slice(0, 12);
    });

    // Guided setup state (for library items with setup fields)
    const guidedSetup = reactive({
      item: null,        // the library item being configured
      fields: {},        // field values keyed by field.key
      busy: false,
      error: '',
      showTutorial: false,
      // Email-specific: 'initial' → 'discovered' → verified & added
      emailPhase: '',       // '' | 'initial' | 'discovered'
      emailProvider: '',    // detected provider name
      emailLookup: null,    // full lookup response (server settings)
      emailVerify: null,    // { imap: 'ok'|'failed'|'skipped', smtp: 'ok'|... }
    });

    function openGuidedSetup(item) {
      guidedSetup.item = item;
      guidedSetup.fields = {};
      guidedSetup.busy = false;
      guidedSetup.error = '';
      guidedSetup.showTutorial = false;
      guidedSetup.emailPhase = item.specType === 'email' ? 'initial' : '';
      guidedSetup.emailProvider = '';
      guidedSetup.emailLookup = null;
      guidedSetup.emailVerify = null;
      // Pre-fill defaults
      for (const f of item.setup.fields) {
        guidedSetup.fields[f.key] = f.default || '';
      }
      // Open the modal if not open, switch to guided step
      if (!addFlow.open) {
        addFlow.open = true;
      }
      addFlow.step = 'guided';
      inlineSearch.value = '';
    }

    async function submitGuidedSetup() {
      const setup = guidedSetup.item.setup;
      const item = guidedSetup.item;

      // Validate required fields
      for (const f of setup.fields) {
        if (f.required && !guidedSetup.fields[f.key]?.trim()) {
          guidedSetup.error = `${f.label} is required.`;
          return;
        }
      }

      guidedSetup.busy = true;
      guidedSetup.error = '';

      try {
        // Verification step
        if (setup.verify) {
          if (setup.verify.method === 'detect') {
            // Kubernetes-style: use /detect endpoint
            const url = (guidedSetup.fields.instance_url || '').trim().replace(/\/$/, '');
            const token = (guidedSetup.fields.token || '').trim();
            const data = await apiClient.detect(url, token);
            const found = (data.detected || []).find(p => p.found && p.status >= 200 && p.status < 300);
            if (!found) {
              const reachable = (data.detected || []).find(p => p.status > 0);
              guidedSetup.error = !reachable && !data.online
                ? 'Could not connect — check the URL.'
                : 'Authentication failed — check your credentials.';
              return;
            }
            // Store detected spec URL for later
            guidedSetup._detectedSpecUrl = found.spec_url;
            guidedSetup._detectedSpecType = found.type;
          } else if (setup.flow === 'oauth2') {
            // OAuth flow — handled separately
            await runGuidedOAuth(setup, item);
            return;
          } else {
            // Standard verify via /verify endpoint
            const body = { service: setup.verify.service };
            if (guidedSetup.fields.instance_url) body.base_url = guidedSetup.fields.instance_url.trim().replace(/\/$/, '');
            if (guidedSetup.fields.token) body.token = guidedSetup.fields.token.trim();
            if (guidedSetup.fields.email) body.email = guidedSetup.fields.email.trim();
            const res = await fetch('/verify', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(body),
            });
            const data = await res.json();
            if (!data.ok) {
              guidedSetup.error = data.error === 'auth_error'
                ? 'Credentials rejected — check your token.'
                : `Verification failed: ${data.error || 'could not connect'}`;
              return;
            }
          }
        }

        // Build the API config from field targets
        const api = blankApi();
        api.name = item.title;
        api.baseUrl = item.baseUrl || '';
        api.specUrl = item.specUrl || '';
        api.type = item.specType || '';
        api.knownService = inferKnownService(api.baseUrl, api.specUrl, api.type);

        // Email-specific guided setup (three-phase: initial → discovered → verified)
        if (item.specType === 'email') {
          const emailAddr = (guidedSetup.fields.email || '').trim();
          const emailPass = (guidedSetup.fields.password || '').trim();
          const apiName = (guidedSetup.fields.name || '').trim() || 'email';

          // Phase 1: DNS lookup to auto-detect provider
          if (guidedSetup.emailPhase === 'initial') {
            if (!emailAddr) {
              guidedSetup.error = 'Email address is required.';
              return;
            }
            if (!emailPass) {
              guidedSetup.error = 'Password is required.';
              return;
            }

            const lookupRes = await fetch('/email/lookup', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ email: emailAddr }),
            });
            const lookup = await lookupRes.json();

            if (lookup.error) {
              guidedSetup.error = lookup.error;
              return;
            }

            // Store the lookup result and move to discovered phase
            guidedSetup.emailLookup = lookup;
            guidedSetup.emailProvider = lookup.provider || 'unknown';
            guidedSetup.emailVerify = null;
            guidedSetup.emailPhase = 'discovered';

            // Pre-fill server fields from lookup (editable by user)
            if (lookup.provider !== 'unknown') {
              guidedSetup.fields.smtp_host = lookup.smtp_host || '';
              guidedSetup.fields.smtp_port = lookup.smtp_port ? String(lookup.smtp_port) : '587';
              guidedSetup.fields.smtp_tls = lookup.smtp_tls || 'starttls';
              guidedSetup.fields.imap_host = lookup.imap_host || '';
              guidedSetup.fields.imap_port = lookup.imap_port ? String(lookup.imap_port) : '993';
            } else {
              // Unknown provider — leave fields blank for manual entry
              guidedSetup.fields.smtp_host = guidedSetup.fields.smtp_host || '';
              guidedSetup.fields.smtp_port = guidedSetup.fields.smtp_port || '587';
              guidedSetup.fields.smtp_tls = guidedSetup.fields.smtp_tls || 'starttls';
              guidedSetup.fields.imap_host = guidedSetup.fields.imap_host || '';
              guidedSetup.fields.imap_port = guidedSetup.fields.imap_port || '993';
            }
            guidedSetup.error = '';
            return;
          }

          // Phase 2: Verify credentials and add
          if (guidedSetup.emailPhase === 'discovered') {
            const smtpHost = (guidedSetup.fields.smtp_host || '').trim();
            const imapHost = (guidedSetup.fields.imap_host || '').trim();
            if (!smtpHost && !imapHost) {
              guidedSetup.error = 'At least SMTP or IMAP server is required.';
              return;
            }

            // Call /email/verify to test actual credentials
            const verifyRes = await fetch('/email/verify', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                email: emailAddr,
                password: emailPass,
                smtp_host: smtpHost,
                smtp_port: parseInt(guidedSetup.fields.smtp_port || '587', 10),
                smtp_tls: (guidedSetup.fields.smtp_tls || 'starttls').trim(),
                imap_host: imapHost,
                imap_port: parseInt(guidedSetup.fields.imap_port || '993', 10),
              }),
            });
            const verify = await verifyRes.json();
            guidedSetup.emailVerify = verify;

            if (!verify.ok) {
              guidedSetup.error = verify.error || 'Credential verification failed.';
              return;
            }

            // Verification succeeded — build API and add
            api.name = apiName;
            api.type = 'email';
            api.emailAddress = emailAddr;
            api.emailPassword = emailPass;
            api.emailProvider = guidedSetup.emailProvider !== 'unknown' ? guidedSetup.emailProvider : 'custom';
            api.emailSmtpHost = smtpHost;
            api.emailSmtpPort = (guidedSetup.fields.smtp_port || '587').trim();
            api.emailSmtpTls = (guidedSetup.fields.smtp_tls || 'starttls').trim();
            api.emailImapHost = imapHost;
            api.emailImapPort = (guidedSetup.fields.imap_port || '993').trim();
            api.detectedOnce = true;
            addApiToProfile(api);
            return;
          }
        }

        for (const f of setup.fields) {
          const val = (guidedSetup.fields[f.key] || '').trim();
          if (!val) continue;
          switch (f.target) {
            case 'base_url': api.baseUrl = val.replace(/\/$/, ''); break;
            case 'auth.token': api.authType = item.authType || 'bearer'; api.bearerToken = val; break;
            case 'auth.username': api.basicUser = val; break;
            case 'auth.password': api.basicPass = val; break;
            case 'auth.client_id': api.oauthClientId = val; break;
            case 'auth.client_secret': api.oauthClientSecret = val; break;
            case 'name': api.name = val; break;
          }
        }

        // Handle Jira's Cloud vs Server auth logic
        if (item.id === 'jira' && api.basicUser && guidedSetup.fields.token) {
          const isCloud = api.baseUrl.includes('.atlassian.net');
          if (isCloud) {
            api.authType = 'basic';
            api.basicPass = guidedSetup.fields.token.trim();
          } else {
            api.authType = 'bearer';
            api.bearerToken = guidedSetup.fields.token.trim();
            api.basicUser = '';
          }
        }

        // Spec URL from template or detection
        if (guidedSetup._detectedSpecUrl) {
          api.specUrl = guidedSetup._detectedSpecUrl;
          api.type = guidedSetup._detectedSpecType || api.type;
        } else if (setup.specUrlTemplate && guidedSetup.fields.instance_url) {
          api.specUrl = setup.specUrlTemplate.replace('{instance_url}', api.baseUrl);
        }

        api.detectedOnce = true;
        addApiToProfile(api);
      } catch (err) {
        guidedSetup.error = 'Error: ' + err.message;
      } finally {
        guidedSetup.busy = false;
      }
    }

    async function runGuidedOAuth(setup, item) {
      // Gmail-style OAuth flow
      const clientId = (guidedSetup.fields.client_id || '').trim();
      const clientSecret = (guidedSetup.fields.client_secret || '').trim();
      const scopes = setup.oauth?.scopes || '';

      try {
        // Step 1: Get OAuth URL
        const startRes = await fetch('/oauth/start', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ client_id: clientId, scopes }),
        });
        const startData = await startRes.json();
        if (!startData.auth_url) { guidedSetup.error = 'Failed to generate OAuth URL.'; return; }

        // Step 2: Open popup
        const popup = window.open(startData.auth_url, 'skyline-oauth', 'width=600,height=700,left=200,top=100');
        if (!popup) { guidedSetup.error = 'Popup blocked — please allow popups.'; return; }

        // Step 3: Listen for callback
        const code = await new Promise((resolve, reject) => {
          const timeout = setTimeout(() => { window.removeEventListener('message', handler); reject(new Error('OAuth timed out.')); }, 300000);
          function handler(event) {
            if (event.origin !== window.location.origin) return;
            if (event.data?.type !== 'skyline-oauth-callback') return;
            clearTimeout(timeout);
            window.removeEventListener('message', handler);
            if (event.data.success && event.data.code) resolve(event.data.code);
            else reject(new Error(event.data.error || 'OAuth failed.'));
          }
          window.addEventListener('message', handler);
        });

        // Step 4: Exchange code for tokens
        const exchangeRes = await fetch('/oauth/exchange', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ code, client_id: clientId, client_secret: clientSecret, redirect_uri: startData.redirect_uri }),
        });
        const exchangeData = await exchangeRes.json();
        if (!exchangeData.ok) { guidedSetup.error = `Token exchange failed: ${exchangeData.error}`; return; }

        // Step 5: Verify
        if (setup.verify) {
          const verifyRes = await fetch('/verify', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ service: setup.verify.service, access_token: exchangeData.access_token }),
          });
          const verifyData = await verifyRes.json();
          if (!verifyData.ok) { guidedSetup.error = `Verification failed: ${verifyData.error}`; return; }
        }

        // Step 6: Build API
        const api = blankApi();
        api.name = (guidedSetup.fields.name || '').trim() || item.title;
        api.baseUrl = item.baseUrl;
        api.specUrl = item.specUrl;
        api.type = item.specType;
        api.knownService = inferKnownService(api.baseUrl, api.specUrl, api.type);
        api.authType = 'oauth2';
        api.oauthClientId = clientId;
        api.oauthClientSecret = clientSecret;
        api.oauthRefreshToken = exchangeData.refresh_token || '';
        api.oauthConnected = true;
        api.detectedOnce = true;
        addApiToProfile(api);
      } catch (err) {
        guidedSetup.error = 'OAuth error: ' + err.message;
      } finally {
        guidedSetup.busy = false;
      }
    }

    function addFromLibraryInline(item) {
      // Custom API — open the URL detect flow
      if (item._custom) {
        openAddFlow();
        pickService('custom');
        return;
      }
      // If this API has guided setup fields, open the guided flow
      if (item.setup && item.setup.fields && item.setup.fields.length > 0) {
        openGuidedSetup(item);
        return;
      }

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
      form.apis.push(api);
      inlineSearch.value = "";
      // Close the modal if it's open
      if (addFlow.open) {
        addFlow.open = false;
      }
      addedToast.value = true;
      setTimeout(() => { addedToast.value = false; }, 1500);
    }

    const showNewProfileModal = ref(false);
    const newProfileName = ref("");
    const newProfileError = ref("");
    const selectedApiId = ref("");
    const expandedProfiles = ref({});
    let isLoadingProfile = false;

    // Modal state
    const showConnectModal = ref(false);
    const connectTab = ref("claude-desktop");
    const configModalApiId = ref("");
    const filterModalApiId = ref("");
    const profileSettingsModal = ref("");

    // Computed: API object for config/filter modals
    const configModalApi = computed(() => configModalApiId.value ? form.apis.find(a => a.id === configModalApiId.value) : null);
    const filterModalApi = computed(() => filterModalApiId.value ? form.apis.find(a => a.id === filterModalApiId.value) : null);

    // View-filter state (outside form.apis to avoid triggering auto-save)
    const filterViewState = reactive({});
    function getFilterView(api) {
      if (!filterViewState[api.id]) {
        filterViewState[api.id] = { searchQuery: "", activeMethodFilters: new Set() };
      }
      return filterViewState[api.id];
    }

    // MCP client connect section
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
        if (data.default) defaultProfile.value = data.default;

        // Auto-select default profile if nothing is active
        if (!activeProfile.value && profiles.value.includes(defaultProfile.value)) {
          await loadProfile(defaultProfile.value);
        }

        // Load session counts, per-profile stats, and profile configs in parallel
        const [sessData, ...profileResults] = await Promise.all([
          fetch('/admin/sessions').then(r => r.ok ? r.json() : { sessions: [] }).catch(() => ({ sessions: [] })),
          ...profiles.value.map(name => Promise.all([
            apiClient.loadProfile(name).catch(() => ({ config: { apis: [] } })),
            fetch(`/admin/stats?profile=${encodeURIComponent(name)}`).then(r => r.ok ? r.json() : null).catch(() => null),
          ]))
        ]);

        const sessionCounts = {};
        for (const s of (sessData.sessions || [])) {
          sessionCounts[s.profile] = (sessionCounts[s.profile] || 0) + 1;
        }

        profiles.value.forEach((name, i) => {
          const [profileData, statsData] = profileResults[i];
          try {
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
            const audit = statsData?.audit_stats || {};
            profileMetadata.value[name] = {
              apiCount: apis.length,
              connectedCount: sessionCounts[name] || 0,
              totalRequests: audit.total_requests || 0,
              successRequests: audit.successful_requests || 0,
              failedRequests: audit.failed_requests || 0,
              tokensIn: audit.est_request_tokens || 0,
              tokensOut: audit.est_response_tokens || 0,
              types: Array.from(apiTypes),
              knownServices: Array.from(knownServices),
              apis: apiList,
            };
          } catch (err) {
            console.warn(`Failed to load metadata for profile ${name}:`, err);
          }
        });
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
      const api = form.apis.find((a) => a.id === id);
      const label = api ? (api.name || api.specUrl || 'this API') : 'this API';
      if (!confirm(`Remove "${label}" from the profile?`)) return;
      form.apis = form.apis.filter((a) => a.id !== id);
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
        form.apis = (cfg.apis || []).map((api) => {
          const specType = api.spec_type || inferType(api.spec_url || "");
          return {
            id: generateUUID(),
            name: api.name || "",
            baseUrl: api.base_url_override || "",
            specUrl: api.spec_url || "",
            type: specType,
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
            // Rate limiting
            rateLimitRpm: api.rate_limit_rpm || "",
            rateLimitRph: api.rate_limit_rph || "",
            rateLimitRpd: api.rate_limit_rpd || "",
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
            knownService: inferKnownService(api.base_url_override || "", api.spec_url || "", specType),
            kubeconfigStatus: null,
            // Email protocol config
            emailAddress: api.email?.address || "",
            emailPassword: api.email?.password || "",
            emailSmtpHost: api.email?.smtp_host || "",
            emailSmtpPort: api.email?.smtp_port ? String(api.email.smtp_port) : "",
            emailSmtpTls: api.email?.smtp_tls || "starttls",
            emailImapHost: api.email?.imap_host || "",
            emailImapPort: api.email?.imap_port ? String(api.email.imap_port) : "",
            emailPop3Host: api.email?.pop3_host || "",
            emailPop3Port: api.email?.pop3_port ? String(api.email.pop3_port) : "",
            emailConnectionMode: api.email?.connection_mode || "basic",
            emailProvider: "",
            showAdvanced: false,
          };
        });

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

    async function selectProfile(name) {
      selectedApiId.value = "";
      showConnectModal.value = false;
      // Toggle: collapse if already active, expand if not
      if (activeProfile.value === name) {
        activeProfile.value = "";
        return;
      }
      expandedProfiles.value = { ...expandedProfiles.value, [name]: true };
      await loadProfile(name);
    }

    function selectApi(apiId) {
      selectedApiId.value = selectedApiId.value === apiId ? "" : apiId;
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
          const isEmail = api.type === 'email';
          return hasName && (hasSpecUrl || isEmail);
        })
        .map((api) => {
          const entry = {
            name: api.name.trim(),
            spec_url: api.specUrl?.trim() || undefined,
            base_url_override: api.baseUrl?.trim() || undefined,
          };
          // Include spec_type for non-inferrable types (grpc, email, jira-rest, etc.)
          if (api.type) {
            entry.spec_type = api.type;
          }
          // Email protocol config
          if (api.type === 'email') {
            delete entry.spec_url; // email doesn't use spec_url
            entry.email = {
              address: api.emailAddress || '',
              password: api.emailPassword || '',
            };
            if (api.emailSmtpHost) {
              entry.email.smtp_host = api.emailSmtpHost;
              if (api.emailSmtpPort) entry.email.smtp_port = parseInt(api.emailSmtpPort);
              if (api.emailSmtpTls) entry.email.smtp_tls = api.emailSmtpTls;
            }
            if (api.emailImapHost) {
              entry.email.imap_host = api.emailImapHost;
              if (api.emailImapPort) entry.email.imap_port = parseInt(api.emailImapPort);
            }
            if (api.emailPop3Host) {
              entry.email.pop3_host = api.emailPop3Host;
              if (api.emailPop3Port) entry.email.pop3_port = parseInt(api.emailPop3Port);
            }
            if (api.emailConnectionMode && api.emailConnectionMode !== 'basic') {
              entry.email.connection_mode = api.emailConnectionMode;
            }
          }
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
          // Include rate limiting if set
          if (api.rateLimitRpm) entry.rate_limit_rpm = parseInt(api.rateLimitRpm);
          if (api.rateLimitRph) entry.rate_limit_rph = parseInt(api.rateLimitRph);
          if (api.rateLimitRpd) entry.rate_limit_rpd = parseInt(api.rateLimitRpd);
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
      const targetName = profileSettingsModal.value || form.profileName;
      if (!targetName) return;
      const apiCount = profileMetadata.value[targetName]?.apiCount ?? (targetName === form.profileName ? form.apis.length : 0);
      if (apiCount > 0) {
        alert(`Cannot delete "${targetName}" — remove all ${apiCount} API(s) first.`);
        return;
      }
      if (!confirm(`Delete profile "${targetName}"? This cannot be undone.`)) return;
      const deletedProfileName = targetName;
      try {
        isBusy.value = true;
        await apiClient.deleteProfile(deletedProfileName);
        await refreshProfiles();

        delete profileMetadata.value[deletedProfileName];

        // Clear form only if we deleted the currently loaded profile
        if (form.profileName === deletedProfileName) {
          form.profileName = "";
          form.profileToken = "";
          form.apis = [];
          originalProfileName.value = "";
          activeProfile.value = "";
        }
        profileSettingsModal.value = "";
        setStatus("ok", `Profile "${deletedProfileName}" deleted.`);
      } catch (err) {
        setStatus("error", err.message);
      } finally {
        isBusy.value = false;
      }
    }

    async function toggleFilterConfig(api) {
      api.showFilterConfig = !api.showFilterConfig;
      if (api.showFilterConfig) {
        // Always use allowlist — set it if not already set
        if (!api.filterMode) api.filterMode = "allowlist";
        await fetchOperations(api);
      }
    }

    async function fetchOperations(api) {
      if (!api.specUrl && !api.name && api.type !== 'email') {
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

        // Pre-select operations: restore saved selections, or default to all selected
        api.selectedOperations.clear();
        if (api.filterOperations.length > 0) {
          // Restore saved allowlist selections
          const savedIds = new Set(api.filterOperations.map(f => f.operation_id).filter(Boolean));
          api.availableOperations.forEach((op) => {
            if (savedIds.has(op.id)) api.selectedOperations.add(op.id);
          });
        } else {
          // No saved filter — default to all selected (all operations allowed)
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
      // Always allowlist — selected operations are what gets exposed
      api.filterMode = "allowlist";
      api.filterOperations = Array.from(api.selectedOperations).map((opId) => ({
        operation_id: opId,
      }));
      const count = api.selectedOperations.size;
      const total = api.availableOperations.length;
      setStatus("ok", `Filter updated: ${count} of ${total} operations allowed.`);
    }

    function clearFilter(api) {
      api.filterMode = "";
      api.filterOperations = [];
      api.selectedOperations.clear();
      api.collapsedGroups = new Set();
      setStatus("ok", "Filter cleared — all operations allowed.");
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
        open: true, step: "pick",
        apiName: "", instanceUrl: "", email: "", token: "",
        customUrl: "", detecting: false, detectError: "", detectResults: [], busy: false, error: "",
        libraryItems: [], libraryLoading: false, libraryError: "", librarySearch: "", libraryCategory: "All",
      });
      inlineSearch.value = "";
      // Pre-load library so popular chips and search are ready
      ensureLibraryLoaded();
    }

    function closeAddFlow() {
      addFlow.open = false;
    }

    function pickService(svc) {
      addFlow.step = svc;
      addFlow.error = "";
      if (svc === "library") loadLibrary();
    }

    function addApiToProfile(api) {
      form.apis.push(api);
      closeAddFlow();
      addedToast.value = true;
      setTimeout(() => { addedToast.value = false; }, 1500);
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

    async function loadLibrary() {
      // Use persistent cache if available
      if (libraryLoaded.value) {
        addFlow.libraryItems = libraryCache.value;
        return;
      }
      if (addFlow.libraryItems.length > 0) return;
      addFlow.libraryLoading = true;
      addFlow.libraryError = "";
      try {
        await ensureLibraryLoaded();
        addFlow.libraryItems = libraryCache.value; // always includes built-ins
        if (libraryLoadError.value) addFlow.libraryError = libraryLoadError.value;
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
          const entry = { name: api.name.trim(), spec_url: api.specUrl?.trim() || undefined };
          if (api.baseUrl?.trim()) entry.base_url_override = api.baseUrl.trim();
          if (api.type)            entry.spec_type = api.type;
          // Email config for export
          if (api.type === 'email') {
            delete entry.spec_url;
            entry.email = { address: api.emailAddress || '', password: api.emailPassword || '' };
            if (api.emailSmtpHost) { entry.email.smtp_host = api.emailSmtpHost; if (api.emailSmtpPort) entry.email.smtp_port = parseInt(api.emailSmtpPort); if (api.emailSmtpTls) entry.email.smtp_tls = api.emailSmtpTls; }
            if (api.emailImapHost) { entry.email.imap_host = api.emailImapHost; if (api.emailImapPort) entry.email.imap_port = parseInt(api.emailImapPort); }
            if (api.emailPop3Host) { entry.email.pop3_host = api.emailPop3Host; if (api.emailPop3Port) entry.email.pop3_port = parseInt(api.emailPop3Port); }
            if (api.emailConnectionMode && api.emailConnectionMode !== 'basic') { entry.email.connection_mode = api.emailConnectionMode; }
          }
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
      defaultProfile,
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
      oauthRedirectHint,
      runAddFlowDetect,
      addFromDetectResult,
      loadLibrary,
      libraryCategories,
      filteredLibraryItems,
      addFromLibrary,
      // Profile tree
      selectedApiId,
      expandedProfiles,
      selectProfile,
      selectApi,
      toggleProfileExpand,
      selectApiBySpecUrl,
      // MCP connect modal
      showConnectModal,
      connectTab,
      // Config / Filter / Settings modals
      configModalApiId,
      filterModalApiId,
      configModalApi,
      filterModalApi,
      profileSettingsModal,
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
      // Guided setup
      guidedSetup,
      openGuidedSetup,
      submitGuidedSetup,
      // Inline library search
      faviconUrl,
      inlineSearch,
      inlineSearchFocused,
      blurInlineSearch,
      inlineSearchResults,
      popularApis,
      libraryLoaded,
      libraryLoading,
      ensureLibraryLoaded,
      addFromLibraryInline,
    };
  },
  template: `
    <div class="profiles-section">
      <!-- Section header -->
      <div class="section-header">
        <h2 class="section-title">Profiles</h2>
        <div style="display:flex; gap:6px; align-items:center;">
          <button class="icon-btn" @click="openImportFlow" title="Import Profile">
            <iconify-icon icon="mdi:import"></iconify-icon>
          </button>
          <button class="icon-btn" @click="openNewProfileModal" title="New Profile">
            <iconify-icon icon="mdi:plus-circle-outline"></iconify-icon>
          </button>
        </div>
      </div>

      <!-- Empty state -->
      <div v-if="profiles.length === 0" style="text-align:center; padding:40px 20px; color:var(--text-dim);">
        <iconify-icon icon="mdi:folder-open-outline" style="font-size:40px; opacity:0.4; margin-bottom:12px; display:block;"></iconify-icon>
        <div style="font-size:14px; margin-bottom:16px;">No profiles yet</div>
        <div style="display:flex; gap:10px; justify-content:center;">
          <button class="primary" @click="openNewProfileModal">
            <iconify-icon icon="mdi:plus-circle-outline"></iconify-icon> Create Profile
          </button>
          <button class="ghost" @click="openImportFlow">
            <iconify-icon icon="mdi:import"></iconify-icon> Import
          </button>
        </div>
      </div>

      <!-- Profile accordions -->
      <div v-for="name in profiles" :key="name" class="profile-accordion">
        <!-- Profile header row -->
        <div class="profile-accordion-header" @click="selectProfile(name)">
          <iconify-icon :icon="activeProfile === name ? 'mdi:chevron-down' : 'mdi:chevron-right'" style="font-size:18px; color:var(--text-dim); flex-shrink:0;"></iconify-icon>
          <iconify-icon icon="mdi:folder-account" style="font-size:16px; color:var(--text-muted); flex-shrink:0;"></iconify-icon>
          <span class="profile-accordion-name">{{ name }}</span>
          <span v-if="name === defaultProfile" class="default-badge">default</span>
          <!-- Spacer -->
          <span style="flex:1;"></span>
          <!-- Stats badges — always on the right -->
          <div class="profile-stats" @click.stop>
            <!-- Requests badge -->
            <span
              class="profile-stat-badge stat-requests"
              :title="'Total requests: ' + (profileMetadata[name]?.totalRequests || 0) + '  Successful: ' + (profileMetadata[name]?.successRequests || 0) + '  Failed: ' + (profileMetadata[name]?.failedRequests || 0)"
            >
              <span class="stat-total">{{ profileMetadata[name]?.totalRequests || 0 }}</span>
              <span class="stat-sep">/</span>
              <span class="stat-success">{{ (profileMetadata[name]?.totalRequests || 0) > 0 ? Math.round((profileMetadata[name]?.successRequests || 0) / profileMetadata[name].totalRequests * 100) : 0 }}%</span>
              <span class="stat-sep">/</span>
              <span class="stat-failed">{{ (profileMetadata[name]?.totalRequests || 0) > 0 ? Math.round((profileMetadata[name]?.failedRequests || 0) / profileMetadata[name].totalRequests * 100) : 0 }}%</span>
              <span class="stat-sep"> </span>Requests
            </span>
            <!-- Tokens badge -->
            <span class="profile-stat-badge">
              in: {{ profileMetadata[name]?.tokensIn || 0 }} / out: {{ profileMetadata[name]?.tokensOut || 0 }} Tokens
            </span>
            <!-- API count -->
            <span class="profile-stat-badge">
              {{ profileMetadata[name]?.apiCount || 0 }} API{{ (profileMetadata[name]?.apiCount || 0) !== 1 ? 's' : '' }}
            </span>
            <!-- Connected -->
            <span class="profile-stat-badge" :class="{ 'stat-connected': (profileMetadata[name]?.connectedCount || 0) > 0 }">
              {{ profileMetadata[name]?.connectedCount || 0 }} Connected
            </span>
          </div>
          <!-- Action buttons (only when expanded) -->
          <div v-if="activeProfile === name" class="profile-accordion-actions" @click.stop>
            <button v-if="form.profileToken" class="ghost small connect-btn" @click="showConnectModal = true" title="Connect AI client">
              <iconify-icon icon="mdi:connection" style="font-size:14px;"></iconify-icon> Connect
            </button>
            <button class="icon-btn" @click="openExportFlow" :disabled="!form.apis.length" title="Export Profile">
              <iconify-icon icon="mdi:export" style="font-size:14px;"></iconify-icon>
            </button>
            <button class="icon-btn" @click="profileSettingsModal = name" title="Profile Settings">
              <iconify-icon icon="mdi:cog-outline" style="font-size:14px;"></iconify-icon>
            </button>
          </div>
        </div>

        <!-- Expanded body -->
        <div v-if="activeProfile === name" class="profile-accordion-body">

          <!-- Add API (first) -->
          <div class="add-api-row" style="margin-top:0; padding-top:0; border-top:none; margin-bottom:12px;">
            <div class="unified-search-input unified-search-compact" :style="inlineSearchFocused ? { borderColor: 'var(--blue)' } : {}">
              <iconify-icon icon="mdi:plus" style="font-size:16px; color:var(--text-dim); flex-shrink:0;"></iconify-icon>
              <input
                v-model="inlineSearch"
                placeholder="Add API — search or paste URL..."
                @focus="inlineSearchFocused = true; ensureLibraryLoaded()"
                @blur="blurInlineSearch()"
                @keyup.enter="inlineSearch.trim().startsWith('http') ? (addFlow.customUrl = inlineSearch.trim(), openAddFlow(), pickService('custom'), inlineSearch = '', runAddFlowDetect()) : null"
                style="flex:1; background:none; border:none; outline:none; color:var(--text); font-size:12px;"
              />
              <iconify-icon v-if="inlineSearch.trim()" icon="mdi:close-circle" style="font-size:14px; color:var(--text-dim); cursor:pointer;" @mousedown.prevent="inlineSearch = ''"></iconify-icon>
            </div>
            <div v-if="inlineSearchFocused" class="unified-search-results">
              <div v-if="inlineSearch.trim().startsWith('http')" class="unified-url-detect">
                <iconify-icon icon="mdi:cloud-search-outline" style="font-size:14px; color:var(--blue);"></iconify-icon>
                <span style="flex:1; font-size:12px;">Detect API at <strong>{{ inlineSearch.trim() }}</strong></span>
                <button class="primary small" @mousedown.prevent="addFlow.customUrl = inlineSearch.trim(); openAddFlow(); pickService('custom'); inlineSearch = ''; runAddFlowDetect()">Detect</button>
              </div>
              <template v-else-if="inlineSearch.trim()">
                <div v-if="libraryLoading && inlineSearchResults.length === 0" style="padding:12px; text-align:center; color:var(--text-dim); font-size:12px;">
                  <iconify-icon icon="mdi:loading" style="animation:spin 1s linear infinite; margin-right:4px;"></iconify-icon> Loading library...
                </div>
                <div v-else-if="inlineSearchResults.length > 0" style="max-height:260px; overflow-y:auto;">
                  <div v-for="item in inlineSearchResults" :key="item.id" class="detect-result-item" @mousedown.prevent="addFromLibraryInline(item); inlineSearch = ''" style="cursor:pointer;">
                    <iconify-icon v-if="item._custom" icon="mdi:plus-circle-outline" style="font-size:18px; flex-shrink:0; color:var(--blue);"></iconify-icon>
                    <img v-else-if="item.website" :src="faviconUrl(item.website)" style="width:18px; height:18px; flex-shrink:0; object-fit:contain; border-radius:3px;" @error="$event.target.style.display='none'" />
                    <div style="flex:1; min-width:0;">
                      <div style="font-weight:500; font-size:12px;">{{ item.title }}</div>
                      <div class="muted" style="font-size:11px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">{{ item.subtitle }}</div>
                    </div>
                    <iconify-icon icon="mdi:plus-circle" style="color:var(--blue); flex-shrink:0;"></iconify-icon>
                  </div>
                </div>
                <div v-else style="padding:12px; text-align:center; color:var(--text-dim); font-size:12px;">No APIs found for "{{ inlineSearch.trim() }}"</div>
              </template>
              <template v-else>
                <div style="max-height:260px; overflow-y:auto;">
                  <div v-for="item in popularApis" :key="item.id" class="detect-result-item" @mousedown.prevent="addFromLibraryInline(item); inlineSearch = ''" style="cursor:pointer;">
                    <iconify-icon v-if="item._custom" icon="mdi:plus-circle-outline" style="font-size:18px; flex-shrink:0; color:var(--blue);"></iconify-icon>
                    <img v-else-if="item.website" :src="faviconUrl(item.website)" style="width:18px; height:18px; flex-shrink:0; object-fit:contain; border-radius:3px;" @error="$event.target.style.display='none'" />
                    <div style="flex:1; min-width:0;">
                      <div style="font-weight:500; font-size:12px;">{{ item.title }}</div>
                      <div class="muted" style="font-size:11px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">{{ item.subtitle }}</div>
                    </div>
                    <iconify-icon icon="mdi:plus-circle" style="color:var(--blue); flex-shrink:0;"></iconify-icon>
                  </div>
                </div>
                <div v-if="libraryLoading" style="padding:8px 14px; font-size:11px; color:var(--text-dim); border-top:1px solid var(--border);">
                  <iconify-icon icon="mdi:loading" style="animation:spin 1s linear infinite; margin-right:4px;"></iconify-icon> Loading more...
                </div>
              </template>
            </div>
          </div>

          <!-- Status / toast -->
          <div v-if="status.message" class="profile-status">
            <span class="status-dot" :class="{ ok: status.state === 'ok', err: status.state === 'error' }"></span>
            <span>{{ status.message }}</span>
          </div>
          <div v-if="addedToast" class="toast fade-in" style="margin-bottom:6px; font-size:12px;">Added to profile</div>

          <!-- API rows -->
          <div v-if="form.apis.length === 0" style="padding:8px 0 4px; text-align:center; color:var(--text-dim); font-size:13px;">
            No APIs yet
          </div>
          <div v-for="api in form.apis" :key="api.id" class="api-row">
            <iconify-icon :icon="serviceIcons[api.knownService] || typeIcons[api.type] || 'mdi:cloud-outline'" style="font-size:22px; flex-shrink:0; color:var(--blue);"></iconify-icon>
            <div class="api-row-info">
              <span class="api-row-name">{{ api.name || api.specUrl || 'Unconfigured' }}</span>
              <span class="api-row-type">{{ api.knownService ? serviceLabels[api.knownService] : (api.type ? typeLabels[api.type] : 'Unknown') }}</span>
            </div>
            <div class="api-row-actions">
              <button class="ghost small" @click="configModalApiId = api.id" title="Configure API">
                <iconify-icon icon="mdi:cog-outline" style="font-size:13px;"></iconify-icon> Config
              </button>
              <button class="ghost small" :class="{ active: api.filterMode }" @click="filterModalApiId = api.id; toggleFilterConfig(api)" title="Operation Filter">
                <iconify-icon icon="mdi:filter-variant" style="font-size:13px;"></iconify-icon>
                <span v-if="api.filterMode" style="color:var(--blue);">{{ api.filterOperations.length }} ops</span>
                <span v-else>Filter</span>
              </button>
              <button class="icon-btn" style="color:var(--text-dim);" @click="removeApi(api.id)" title="Remove API">
                <iconify-icon icon="mdi:close" style="font-size:14px;"></iconify-icon>
              </button>
            </div>
          </div>

        </div>
      </div>

      <!-- Connect Modal: tabbed snippet viewer -->
      <div v-if="showConnectModal" class="modal-backdrop" @click.self="showConnectModal = false">
        <div class="modal-card connect-modal">
          <div class="modal-header">
            <iconify-icon icon="mdi:connection" style="font-size:20px; color:var(--blue);"></iconify-icon>
            <span>Connect to <strong>{{ activeProfile }}</strong></span>
            <button class="icon-btn" @click="showConnectModal = false" style="margin-left:auto;">
              <iconify-icon icon="mdi:close"></iconify-icon>
            </button>
          </div>
          <div class="connect-tabs">
            <button :class="['connect-tab', { active: connectTab === 'claude-desktop' }]" @click="connectTab = 'claude-desktop'">
              <iconify-icon icon="simple-icons:anthropic" style="font-size:12px; color:#d97757;"></iconify-icon>
              Desktop
            </button>
            <button :class="['connect-tab', { active: connectTab === 'claude-code' }]" @click="connectTab = 'claude-code'">
              <iconify-icon icon="mdi:console-line" style="font-size:12px; color:#d97757;"></iconify-icon>
              Code CLI
            </button>
            <button :class="['connect-tab', { active: connectTab === 'cline' }]" @click="connectTab = 'cline'">
              <iconify-icon icon="mdi:puzzle-outline" style="font-size:12px; color:#8b5cf6;"></iconify-icon>
              Cline
            </button>
            <button :class="['connect-tab', { active: connectTab === 'codex' }]" @click="connectTab = 'codex'">
              <iconify-icon icon="simple-icons:openai" style="font-size:12px; color:#74aa9c;"></iconify-icon>
              Codex
            </button>
          </div>
          <div class="connect-tab-content">
            <!-- Claude Desktop -->
            <template v-if="connectTab === 'claude-desktop'">
              <p class="connect-hint">Add to <code>~/Library/Application Support/Claude/claude_desktop_config.json</code> (macOS) or <code>%APPDATA%\\Claude\\claude_desktop_config.json</code> (Windows):</p>
              <pre class="connect-snippet">{{ claudeDesktopSnippet }}</pre>
              <button class="ghost small connect-copy" @click="copySnippet(claudeDesktopSnippet)">
                <iconify-icon icon="mdi:content-copy"></iconify-icon> Copy
              </button>
            </template>
            <!-- Claude Code -->
            <template v-else-if="connectTab === 'claude-code'">
              <p class="connect-hint">Run in your terminal:</p>
              <pre class="connect-snippet">{{ claudeCodeCmd }}</pre>
              <button class="ghost small connect-copy" @click="copySnippet(claudeCodeCmd)">
                <iconify-icon icon="mdi:content-copy"></iconify-icon> Copy command
              </button>
              <p class="connect-hint" style="margin-top:12px;">Or add to <code>~/.claude/settings.json</code>:</p>
              <pre class="connect-snippet">{{ claudeCodeSettings }}</pre>
              <button class="ghost small connect-copy" @click="copySnippet(claudeCodeSettings)">
                <iconify-icon icon="mdi:content-copy"></iconify-icon> Copy JSON
              </button>
            </template>
            <!-- Cline -->
            <template v-else-if="connectTab === 'cline'">
              <p class="connect-hint">In Cline, go to <strong>MCP Servers &rarr; Add Server</strong>, or edit <code>cline_mcp_settings.json</code>:</p>
              <pre class="connect-snippet">{{ clineSnippet }}</pre>
              <button class="ghost small connect-copy" @click="copySnippet(clineSnippet)">
                <iconify-icon icon="mdi:content-copy"></iconify-icon> Copy
              </button>
            </template>
            <!-- Codex CLI -->
            <template v-else-if="connectTab === 'codex'">
              <p class="connect-hint">Add to <code>~/.codex/config.toml</code> (global) or <code>.codex/config.toml</code> in your project:</p>
              <pre class="connect-snippet">{{ codexSnippet }}</pre>
              <button class="ghost small connect-copy" @click="copySnippet(codexSnippet)">
                <iconify-icon icon="mdi:content-copy"></iconify-icon> Copy
              </button>
            </template>
          </div>
        </div>
      </div>

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

              <!-- Library search -->
              <div style="margin-bottom:16px;">
                <div style="display:flex; align-items:center; gap:8px; background:var(--bg-input); border:1px solid var(--border); border-radius:var(--radius); padding:10px 12px; transition:border-color .15s;" :style="inlineSearchFocused ? { borderColor: 'var(--blue)' } : {}">
                  <iconify-icon icon="mdi:magnify" style="font-size:18px; color:var(--text-dim); flex-shrink:0;"></iconify-icon>
                  <input
                    v-model="inlineSearch"
                    placeholder="Search 18,000+ APIs..."
                    @focus="inlineSearchFocused = true; ensureLibraryLoaded()"
                    @blur="inlineSearchFocused = false"
                    style="flex:1; background:none; border:none; outline:none; color:var(--text); font-size:14px;"
                  />
                  <iconify-icon v-if="inlineSearch.trim()" icon="mdi:close-circle" style="font-size:16px; color:var(--text-dim); cursor:pointer; flex-shrink:0;" @click="inlineSearch = ''"></iconify-icon>
                </div>
              </div>

              <!-- Search results (inline, not dropdown) -->
              <template v-if="inlineSearch.trim()">
                <div v-if="inlineSearchResults.length > 0" style="max-height:320px; overflow-y:auto; margin-bottom:12px;">
                  <div
                    v-for="item in inlineSearchResults"
                    :key="item.id"
                    class="detect-result-item"
                    @click="addFromLibraryInline(item)"
                    style="cursor:pointer;"
                  >
                    <img
                      v-if="item.website"
                      :src="faviconUrl(item.website)"
                      :alt="item.title"
                      style="width:22px; height:22px; flex-shrink:0; object-fit:contain; border-radius:4px;"
                      @error="$event.target.style.display='none'"
                    />
                    <span
                      v-else
                      style="width:22px; height:22px; flex-shrink:0; display:flex; align-items:center; justify-content:center; border-radius:4px; background:var(--bg-input); color:var(--text-dim); font-size:11px; font-weight:600;"
                    >{{ item.title.trim().charAt(0).toUpperCase() }}</span>
                    <div style="flex:1; min-width:0;">
                      <div style="font-weight:500; font-size:13px;">{{ item.title }}</div>
                      <div class="muted" style="font-size:11px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">{{ item.subtitle }}</div>
                    </div>
                    <span style="font-size:10px; padding:2px 6px; border-radius:99px; background:var(--bg-input); color:var(--text-dim); border:1px solid var(--border); flex-shrink:0;">{{ item.category }}</span>
                    <iconify-icon icon="mdi:plus-circle" style="color:var(--blue); flex-shrink:0;"></iconify-icon>
                  </div>
                </div>
                <div v-else-if="libraryLoaded" style="padding:20px; text-align:center; color:var(--text-dim); font-size:13px;">
                  No APIs found for "{{ inlineSearch }}"
                </div>
                <div v-else style="padding:20px; text-align:center; color:var(--text-dim); font-size:13px;">
                  Loading library...
                </div>
                <!-- Browse full library link (always visible when searching) -->
                <div style="text-align:center; margin-bottom:8px;">
                  <button class="ghost" @click="pickService('library')" style="font-size:12px; padding:4px 8px; color:var(--blue);">
                    Browse full library with categories
                    <iconify-icon icon="mdi:arrow-right" style="font-size:14px; vertical-align:middle;"></iconify-icon>
                  </button>
                </div>
              </template>

              <!-- Default view: popular + services (when not searching) -->
              <template v-else>
                <!-- Popular quick-picks -->
                <div v-if="popularApis.length > 0" style="margin-bottom:16px;">
                  <div style="font-size:11px; text-transform:uppercase; letter-spacing:0.5px; color:var(--text-dim); margin-bottom:8px; font-weight:600;">Popular</div>
                  <div style="display:flex; flex-wrap:wrap; gap:6px;">
                    <button
                      v-for="item in popularApis"
                      :key="item.id"
                      class="ghost"
                      @click="addFromLibraryInline(item)"
                      style="display:flex; align-items:center; gap:5px; padding:6px 10px; font-size:12px; border-radius:var(--radius);"
                    >
                      <img
                        v-if="item.website"
                      :src="faviconUrl(item.website)"
                      :alt="item.title"
                      style="width:14px; height:14px; object-fit:contain; border-radius:2px;"
                        @error="$event.target.style.display='none'"
                      />
                      {{ item.title }}
                    </button>
                  </div>
                </div>

                <!-- Browse full library link -->
                <div style="text-align:right; margin-bottom:16px;">
                  <button class="ghost" @click="pickService('library')" style="font-size:12px; padding:4px 8px; color:var(--blue);">
                    Browse full library
                    <iconify-icon icon="mdi:arrow-right" style="font-size:14px; vertical-align:middle;"></iconify-icon>
                  </button>
                </div>

                <!-- Divider -->
                <div style="display:flex; align-items:center; gap:12px; margin-bottom:16px;">
                  <div style="flex:1; height:1px; background:rgba(255,255,255,0.08);"></div>
                  <span style="font-size:11px; color:var(--text-dim);">or configure manually</span>
                  <div style="flex:1; height:1px; background:rgba(255,255,255,0.08);"></div>
                </div>

                <!-- Custom API button -->
                <div class="service-picker-grid">
                  <button class="service-pick-btn service-pick-custom" @click="pickService('custom')">
                    <iconify-icon icon="mdi:cloud-search-outline" style="font-size:28px;"></iconify-icon>
                    <span>Custom API</span>
                  </button>
                </div>
              </template>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="closeAddFlow">Cancel</button>
            </div>
          </template>

          <!-- Step: guided setup (library items with setup fields) -->
          <template v-else-if="addFlow.step === 'guided' && guidedSetup.item">
            <div class="modal-header">
              <img
                v-if="guidedSetup.item.website"
                :src="faviconUrl(guidedSetup.item.website)"
                style="width:22px; height:22px; object-fit:contain; border-radius:4px;"
                @error="$event.target.style.display='none'"
              />
              <iconify-icon v-else icon="mdi:api" style="font-size:22px; color:var(--blue);"></iconify-icon>
              <span>Add {{ guidedSetup.item.title }}</span>
            </div>
            <div class="modal-body">
              <!-- Email discovered phase: show provider + server settings + verification -->
              <template v-if="guidedSetup.emailPhase === 'discovered'">
                <!-- Provider discovery result -->
                <div
                  style="padding:10px 12px; border-radius:var(--radius); margin-bottom:16px; font-size:12px; display:flex; align-items:center; gap:8px;"
                  :style="guidedSetup.emailProvider !== 'unknown'
                    ? { background: 'rgba(16,185,129,0.08)', border: '1px solid rgba(16,185,129,0.25)', color: 'var(--text-muted)' }
                    : { background: 'rgba(255,200,0,0.08)', border: '1px solid rgba(255,200,0,0.2)', color: 'var(--text-muted)' }"
                >
                  <iconify-icon
                    :icon="guidedSetup.emailProvider !== 'unknown' ? 'mdi:check-circle' : 'mdi:information-outline'"
                    :style="{ color: guidedSetup.emailProvider !== 'unknown' ? 'var(--green)' : 'var(--orange)', fontSize: '16px' }"
                  ></iconify-icon>
                  <span v-if="guidedSetup.emailProvider !== 'unknown'">
                    Detected <strong>{{ guidedSetup.emailProvider }}</strong> for {{ guidedSetup.fields.email }}
                  </span>
                  <span v-else>
                    Could not auto-detect provider for <strong>{{ guidedSetup.fields.email }}</strong>. Please enter server details manually.
                  </span>
                </div>

                <!-- Verification result (shown after verify attempt) -->
                <div v-if="guidedSetup.emailVerify" style="margin-bottom:16px;">
                  <div style="display:flex; gap:12px; font-size:12px;">
                    <span style="display:flex; align-items:center; gap:4px;">
                      <iconify-icon
                        :icon="guidedSetup.emailVerify.imap === 'ok' ? 'mdi:check-circle' : guidedSetup.emailVerify.imap === 'skipped' ? 'mdi:minus-circle' : 'mdi:close-circle'"
                        :style="{ color: guidedSetup.emailVerify.imap === 'ok' ? 'var(--green)' : guidedSetup.emailVerify.imap === 'skipped' ? 'var(--text-dim)' : 'var(--red)' }"
                      ></iconify-icon>
                      IMAP: {{ guidedSetup.emailVerify.imap }}
                    </span>
                    <span style="display:flex; align-items:center; gap:4px;">
                      <iconify-icon
                        :icon="guidedSetup.emailVerify.smtp === 'ok' ? 'mdi:check-circle' : guidedSetup.emailVerify.smtp === 'skipped' ? 'mdi:minus-circle' : 'mdi:close-circle'"
                        :style="{ color: guidedSetup.emailVerify.smtp === 'ok' ? 'var(--green)' : guidedSetup.emailVerify.smtp === 'skipped' ? 'var(--text-dim)' : 'var(--red)' }"
                      ></iconify-icon>
                      SMTP: {{ guidedSetup.emailVerify.smtp }}
                    </span>
                  </div>
                </div>

                <!-- Editable server settings -->
                <div style="font-size:11px; text-transform:uppercase; letter-spacing:0.5px; color:var(--text-dim); margin-bottom:8px; font-weight:600;">Server Settings</div>
                <div style="display:grid; grid-template-columns:1fr auto; gap:8px; margin-bottom:12px;">
                  <div>
                    <label>SMTP Host <span style="color:var(--text-dim); font-weight:400;">(sending)</span></label>
                    <input type="text" v-model="guidedSetup.fields.smtp_host" placeholder="smtp.example.com" />
                  </div>
                  <div>
                    <label>Port</label>
                    <input type="text" v-model="guidedSetup.fields.smtp_port" placeholder="587" style="width:70px;" />
                  </div>
                </div>
                <div style="margin-bottom:12px;">
                  <label>SMTP Security</label>
                  <select v-model="guidedSetup.fields.smtp_tls" style="width:150px;">
                    <option value="starttls">STARTTLS</option>
                    <option value="tls">TLS/SSL</option>
                    <option value="none">None</option>
                  </select>
                </div>
                <div style="display:grid; grid-template-columns:1fr auto; gap:8px; margin-bottom:12px;">
                  <div>
                    <label>IMAP Host <span style="color:var(--text-dim); font-weight:400;">(reading)</span></label>
                    <input type="text" v-model="guidedSetup.fields.imap_host" placeholder="imap.example.com" />
                  </div>
                  <div>
                    <label>Port</label>
                    <input type="text" v-model="guidedSetup.fields.imap_port" placeholder="993" style="width:70px;" />
                  </div>
                </div>
              </template>

              <!-- Standard fields (non-email or email initial phase) -->
              <template v-else>
                <div v-for="field in guidedSetup.item.setup.fields" :key="field.key" style="margin-bottom:12px;">
                  <label>{{ field.label }} <span v-if="field.required" style="color:var(--red);">*</span></label>
                  <input
                    v-if="field.type === 'password'"
                    type="password"
                    v-model="guidedSetup.fields[field.key]"
                    :placeholder="field.placeholder || ''"
                  />
                  <input
                    v-else-if="field.type === 'url'"
                    type="url"
                    v-model="guidedSetup.fields[field.key]"
                    :placeholder="field.placeholder || ''"
                  />
                  <input
                    v-else
                    type="text"
                    v-model="guidedSetup.fields[field.key]"
                    :placeholder="field.placeholder || ''"
                  />
                </div>
              </template>

              <!-- Tutorial toggle -->
              <div v-if="guidedSetup.item.setup.tutorial" style="margin-top:16px;">
                <div
                  @click="guidedSetup.showTutorial = !guidedSetup.showTutorial"
                  style="display:flex; align-items:center; gap:6px; cursor:pointer; font-size:13px; color:var(--blue);"
                >
                  <iconify-icon :icon="guidedSetup.showTutorial ? 'mdi:chevron-down' : 'mdi:chevron-right'"></iconify-icon>
                  <span>How to get credentials</span>
                </div>
                <div v-if="guidedSetup.showTutorial" style="margin-top:8px; padding:12px; background:var(--bg-input); border-radius:var(--radius); font-size:12px; line-height:1.6; color:var(--text-muted); white-space:pre-wrap;">{{ guidedSetup.item.setup.tutorial }}</div>
              </div>

              <div v-if="guidedSetup.error" class="modal-error" style="margin-top:12px;">{{ guidedSetup.error }}</div>
            </div>
            <div class="modal-footer">
              <button class="ghost" @click="guidedSetup.emailPhase === 'discovered' ? (guidedSetup.emailPhase = 'initial', guidedSetup.emailVerify = null, guidedSetup.error = '') : (addFlow.step = 'pick')">Back</button>
              <button class="primary" :disabled="guidedSetup.busy" @click="submitGuidedSetup">
                <iconify-icon v-if="guidedSetup.busy" icon="mdi:loading" style="animation:spin 1s linear infinite;"></iconify-icon>
                {{ guidedSetup.busy
                  ? (guidedSetup.emailPhase === 'initial' ? 'Detecting...' : 'Verifying...')
                  : guidedSetup.emailPhase === 'initial' ? 'Detect Provider'
                  : guidedSetup.emailPhase === 'discovered' ? 'Verify & Add'
                  : 'Add ' + guidedSetup.item.title }}
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
                    :src="faviconUrl(item.website)"
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

      <!-- Profile Settings Modal -->
      <div v-if="profileSettingsModal" class="modal-backdrop" @click.self="profileSettingsModal = ''">
        <div class="modal-card" style="max-width:400px;">
          <div class="modal-header">
            <iconify-icon icon="mdi:folder-cog-outline" style="font-size:20px; color:var(--blue);"></iconify-icon>
            <span>Profile Settings — {{ profileSettingsModal }}</span>
          </div>
          <div class="modal-body">
            <div style="margin-bottom:12px;">
              <label>Profile name</label>
              <input v-model="form.profileName" placeholder="dev, prod, agent-a" />
            </div>
            <div style="margin-bottom:12px;">
              <label>Profile token</label>
              <div v-if="form.profileToken" style="position:relative;">
                <input :value="form.profileToken" :type="showToken ? 'text' : 'password'" disabled style="width:100%; padding-right:36px;" />
                <button class="token-toggle" @click="toggleTokenVisibility" type="button">
                  <iconify-icon :icon="showToken ? 'mdi:eye-off' : 'mdi:eye'" style="font-size:14px;"></iconify-icon>
                </button>
              </div>
              <div v-else class="token-placeholder" style="font-size:12px;">
                <iconify-icon icon="mdi:information-outline"></iconify-icon>
                <span>Save profile to generate token</span>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <button v-if="activeProfile !== defaultProfile" class="ghost" style="color:var(--red);" :disabled="isBusy" @click="deleteProfile().then(() => { if (!activeProfile) profileSettingsModal = '' })">Delete</button>
            <button class="primary" :disabled="isBusy" @click="saveProfile(); profileSettingsModal = ''">Save</button>
          </div>
        </div>
      </div>

      <!-- API Config Modal -->
      <div v-if="configModalApi" class="modal-backdrop" @click.self="configModalApiId = ''">
        <div class="modal-card" style="max-width:560px; width:95%;">
          <div class="modal-header">
            <iconify-icon :icon="serviceIcons[configModalApi.knownService] || typeIcons[configModalApi.type] || 'mdi:cloud-outline'" style="font-size:20px;"></iconify-icon>
            <span>{{ configModalApi.name || configModalApi.specUrl || 'API Config' }}</span>
            <div style="margin-left:auto; display:flex; gap:6px;">
              <button class="secondary small" :disabled="isBusy" @click="detectApi(configModalApi)">Re-detect</button>
              <button class="secondary small" :disabled="isBusy" @click="testApi(configModalApi)">Test</button>
            </div>
          </div>
          <div class="modal-body" style="max-height:70vh; overflow-y:auto;">
            <!-- Known services: read-only connection info -->
            <div v-if="configModalApi.knownService" class="form-grid">
              <div><label>Base URL</label><input :value="configModalApi.baseUrl" readonly class="input-readonly" /></div>
              <div><label>API name</label><input v-model="configModalApi.name" placeholder="domain - API type" /></div>
            </div>

            <!-- Known-service credential helpers -->
            <div v-if="configModalApi.knownService === 'gitlab'" class="credential-helper">
              <div class="helper-header"><iconify-icon icon="simple-icons:gitlab"></iconify-icon>GitLab Authentication</div>
              <p class="helper-desc">Personal Access Token (requires <code>api</code> scope).</p>
              <div style="position:relative;">
                <input :type="configModalApi.showSecret ? 'text' : 'password'" placeholder="glpat-xxxxxxxxxxxxxxxxxxxx" :value="configModalApi.bearerToken"
                  @input="configModalApi.authType='bearer'; configModalApi.bearerToken=$event.target.value" style="width:100%; padding-right:40px;" />
                <button class="token-toggle" @click="configModalApi.showSecret = !configModalApi.showSecret" type="button">
                  <iconify-icon :icon="configModalApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon>
                </button>
              </div>
            </div>
            <div v-if="configModalApi.knownService === 'jira'" class="credential-helper">
              <div class="helper-header"><iconify-icon icon="simple-icons:jira"></iconify-icon>Jira Authentication</div>
              <template v-if="configModalApi.authType === 'basic'">
                <p class="helper-desc">Email &amp; API Token (Jira Cloud).</p>
                <div class="form-grid">
                  <div><label>Email</label><input v-model="configModalApi.basicUser" placeholder="you@company.com" /></div>
                  <div><label>API Token</label>
                    <div style="position:relative;"><input :type="configModalApi.showSecret ? 'text' : 'password'" :value="configModalApi.basicPass" @input="configModalApi.basicPass=$event.target.value" style="width:100%; padding-right:40px;" />
                      <button class="token-toggle" @click="configModalApi.showSecret = !configModalApi.showSecret" type="button"><iconify-icon :icon="configModalApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon></button></div>
                  </div>
                </div>
              </template>
              <template v-else>
                <p class="helper-desc">Personal Access Token.</p>
                <div style="position:relative;"><input :type="configModalApi.showSecret ? 'text' : 'password'" placeholder="Your Jira PAT" :value="configModalApi.bearerToken" @input="configModalApi.authType='bearer'; configModalApi.bearerToken=$event.target.value" style="width:100%; padding-right:40px;" />
                  <button class="token-toggle" @click="configModalApi.showSecret = !configModalApi.showSecret" type="button"><iconify-icon :icon="configModalApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon></button></div>
              </template>
            </div>
            <div v-if="configModalApi.knownService === 'slack'" class="credential-helper">
              <div class="helper-header"><iconify-icon icon="simple-icons:slack"></iconify-icon>Slack Authentication</div>
              <p class="helper-desc">
                <span v-if="slackTokenType(configModalApi.bearerToken) === 'bot'">Bot token detected</span>
                <span v-else-if="slackTokenType(configModalApi.bearerToken) === 'user'">User token detected</span>
                <span v-else>Enter an <code>xoxb-</code> Bot Token or <code>xoxp-</code> User Token</span>
              </p>
              <div style="position:relative;"><input :type="configModalApi.showSecret ? 'text' : 'password'" placeholder="xoxb-... or xoxp-..." :value="configModalApi.bearerToken" @input="configModalApi.authType='bearer'; configModalApi.bearerToken=$event.target.value" style="width:100%; padding-right:40px;" />
                <button class="token-toggle" @click="configModalApi.showSecret = !configModalApi.showSecret" type="button"><iconify-icon :icon="configModalApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon></button></div>
            </div>
            <div v-if="configModalApi.knownService === 'gmail'" class="credential-helper">
              <div class="helper-header"><iconify-icon icon="simple-icons:gmail"></iconify-icon>Gmail (OAuth 2.0)</div>
              <div v-if="configModalApi.oauthConnected" style="display:flex; align-items:center; gap:6px; margin:8px 0;">
                <iconify-icon icon="mdi:check-circle" style="color:var(--green); font-size:16px;"></iconify-icon>
                <span>Connected<span v-if="configModalApi.oauthEmail"> as {{ configModalApi.oauthEmail }}</span></span>
              </div>
              <div v-else style="display:flex; align-items:center; gap:6px; margin:8px 0;">
                <iconify-icon icon="mdi:alert-circle" style="color:var(--red); font-size:16px;"></iconify-icon>
                <span>Not connected — re-add this API to authorize</span>
              </div>
            </div>

            <!-- Email protocol settings -->
            <template v-if="configModalApi.type === 'email'">
              <div class="form-grid">
                <div><label>API Name</label><input v-model="configModalApi.name" placeholder="email" /></div>
                <div><label>Provider</label><input :value="configModalApi.emailProvider || 'custom'" readonly class="input-readonly" /></div>
              </div>
              <div class="credential-helper" style="margin-top:12px;">
                <div class="helper-header"><iconify-icon icon="mdi:email-outline"></iconify-icon>Email Account</div>
                <div class="form-grid">
                  <div><label>Email</label><input v-model="configModalApi.emailAddress" placeholder="you@example.com" /></div>
                  <div><label>Password</label>
                    <div style="position:relative;"><input :type="configModalApi.showSecret ? 'text' : 'password'" v-model="configModalApi.emailPassword" placeholder="App password" style="width:100%; padding-right:40px;" />
                      <button class="token-toggle" @click="configModalApi.showSecret = !configModalApi.showSecret" type="button"><iconify-icon :icon="configModalApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon></button></div>
                  </div>
                </div>
              </div>
              <div style="margin-top:12px; font-size:13px; font-weight:600; margin-bottom:8px;"><iconify-icon icon="mdi:server-network"></iconify-icon> Server Settings</div>
              <div class="form-grid">
                <div><label>SMTP Host</label><input v-model="configModalApi.emailSmtpHost" /></div>
                <div><label>SMTP Port</label><input v-model="configModalApi.emailSmtpPort" style="width:80px;" /></div>
              </div>
              <div class="form-grid" style="margin-top:8px;">
                <div><label>IMAP Host</label><input v-model="configModalApi.emailImapHost" /></div>
                <div><label>IMAP Port</label><input v-model="configModalApi.emailImapPort" style="width:80px;" /></div>
              </div>
              <div style="margin-top:12px;"><div style="font-size:13px; font-weight:600; margin-bottom:8px;"><iconify-icon icon="mdi:connection"></iconify-icon> Connection Mode</div>
                <div style="display:flex; gap:12px;">
                  <label style="display:flex; align-items:center; gap:6px; cursor:pointer; font-size:13px;"><input type="radio" v-model="configModalApi.emailConnectionMode" value="basic" /> Basic</label>
                  <label style="display:flex; align-items:center; gap:6px; cursor:pointer; font-size:13px;"><input type="radio" v-model="configModalApi.emailConnectionMode" value="persistent" /> Persistent</label>
                </div>
              </div>
            </template>

            <!-- Generic APIs -->
            <template v-if="!configModalApi.knownService && configModalApi.type !== 'email'">
              <div class="form-grid">
                <div><label>Base URL</label><input v-model="configModalApi.baseUrl" @blur="detectOnBlur(configModalApi)" /></div>
                <div><label>API name</label><input v-model="configModalApi.name" /></div>
              </div>
              <div class="form-grid">
                <div><label>Spec URL</label><input v-model="configModalApi.specUrl" placeholder="autofilled after detect" /></div>
                <div><label>Auth type</label>
                  <select v-model="configModalApi.authType">
                    <option value="none">None</option><option value="bearer">Bearer</option><option value="basic">Basic</option><option value="api-key">API Key</option>
                  </select>
                </div>
              </div>
              <div v-if="configModalApi.authType === 'bearer'" class="form-grid">
                <div><label>Bearer token</label>
                  <div style="position:relative;"><input v-model="configModalApi.bearerToken" :type="configModalApi.showSecret ? 'text' : 'password'" style="width:100%; padding-right:40px;" />
                    <button class="token-toggle" @click="configModalApi.showSecret = !configModalApi.showSecret" type="button"><iconify-icon :icon="configModalApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon></button></div>
                </div>
              </div>
              <div v-if="configModalApi.authType === 'basic'" class="form-grid">
                <div><label>Username</label><input v-model="configModalApi.basicUser" /></div>
                <div><label>Password</label>
                  <div style="position:relative;"><input v-model="configModalApi.basicPass" :type="configModalApi.showSecret ? 'text' : 'password'" style="width:100%; padding-right:40px;" />
                    <button class="token-toggle" @click="configModalApi.showSecret = !configModalApi.showSecret" type="button"><iconify-icon :icon="configModalApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon></button></div>
                </div>
              </div>
              <div v-if="configModalApi.authType === 'api-key'" class="form-grid">
                <div><label>Header</label><input v-model="configModalApi.apiKeyHeader" /></div>
                <div><label>Value</label>
                  <div style="position:relative;"><input v-model="configModalApi.apiKeyValue" :type="configModalApi.showSecret ? 'text' : 'password'" style="width:100%; padding-right:40px;" />
                    <button class="token-toggle" @click="configModalApi.showSecret = !configModalApi.showSecret" type="button"><iconify-icon :icon="configModalApi.showSecret ? 'mdi:eye-off' : 'mdi:eye'"></iconify-icon></button></div>
                </div>
              </div>
            </template>

            <!-- Rate Limiting & Response Size -->
            <div style="margin-top:16px; border-top:1px solid var(--border); padding-top:14px;">
              <div style="font-size:13px; font-weight:600; margin-bottom:8px;"><iconify-icon icon="mdi:speedometer"></iconify-icon> Rate Limiting</div>
              <div class="form-grid" style="grid-template-columns: repeat(3, 1fr);">
                <div><label>/ Minute</label><input v-model="configModalApi.rateLimitRpm" type="number" min="0" placeholder="0" /></div>
                <div><label>/ Hour</label><input v-model="configModalApi.rateLimitRph" type="number" min="0" placeholder="0" /></div>
                <div><label>/ Day</label><input v-model="configModalApi.rateLimitRpd" type="number" min="0" placeholder="0" /></div>
              </div>
              <div class="form-grid" style="margin-top:12px;">
                <div><label>Max Response Size</label><input v-model="configModalApi.maxResponseBytes" type="number" min="0" step="1024" placeholder="Default (50KB)" />
                  <div class="muted" style="font-size:11px; margin-top:4px;">Bytes. 0 = no limit.</div>
                </div>
              </div>
            </div>
          </div>
          <div class="modal-footer">
            <span style="font-size:11px; color:var(--text-dim);">Changes auto-save</span>
            <button class="ghost" @click="configModalApiId = ''" style="margin-left:auto;">Close</button>
          </div>
        </div>
      </div>

      <!-- API Filter Modal -->
      <div v-if="filterModalApi" class="modal-backdrop" @click.self="filterModalApiId = ''">
        <div class="modal-card" style="max-width:640px; width:95%;">
          <div class="modal-header">
            <iconify-icon icon="mdi:filter-variant" style="font-size:20px; color:var(--blue);"></iconify-icon>
            <span>Operation Filter — {{ filterModalApi.name || filterModalApi.specUrl }}</span>
          </div>
          <div class="modal-body" style="max-height:70vh; overflow-y:auto;">
            <div v-if="filterModalApi.filterLoading" class="loading-state" style="padding:30px; text-align:center;">
              <iconify-icon icon="mdi:loading"></iconify-icon>
              <div style="margin-top:8px;">Loading operations...</div>
            </div>
            <div v-else-if="filterModalApi.availableOperations.length > 0">
              <div class="filter-toolbar">
                <div class="method-pills">
                  <button v-for="method in allMethodsForApi(filterModalApi)" :key="method" class="method-pill" :class="[method.toLowerCase(), { inactive: !isMethodActive(filterModalApi, method) }]" @click="toggleMethodFilter(filterModalApi, method)">{{ method }}</button>
                </div>
                <div class="filter-search-wrapper">
                  <iconify-icon icon="mdi:magnify" class="filter-search-icon"></iconify-icon>
                  <input type="text" class="filter-search-input" placeholder="Search operations..." :value="getFilterView(filterModalApi).searchQuery" @input="getFilterView(filterModalApi).searchQuery = $event.target.value" />
                  <button v-if="getFilterView(filterModalApi).searchQuery" class="filter-search-clear" @click="clearSearch(filterModalApi)"><iconify-icon icon="mdi:close-circle"></iconify-icon></button>
                </div>
                <div class="filter-bulk-actions">
                  <span class="filter-visible-count">{{ filteredOperations(filterModalApi).length }}/{{ filterModalApi.availableOperations.length }} visible · {{ filterModalApi.selectedOperations.size }} selected</span>
                  <button class="ghost small" @click="selectVisible(filterModalApi)"><iconify-icon icon="mdi:checkbox-multiple-marked"></iconify-icon> Select all</button>
                  <button class="ghost small" @click="deselectVisible(filterModalApi)"><iconify-icon icon="mdi:checkbox-multiple-blank-outline"></iconify-icon> Deselect all</button>
                </div>
              </div>
              <div class="operations-list">
                <div v-for="group in filteredGroups(filterModalApi)" :key="group.name" class="op-group">
                  <div class="op-group-header" @click="toggleGroup(filterModalApi, group.name)">
                    <input type="checkbox" class="operation-checkbox" :checked="group.operations.every(op => filterModalApi.selectedOperations.has(op.id))" :indeterminate.prop="group.operations.some(op => filterModalApi.selectedOperations.has(op.id)) && !group.operations.every(op => filterModalApi.selectedOperations.has(op.id))" @click.stop="toggleGroupSelection(filterModalApi, group.operations)" />
                    <iconify-icon :icon="filterModalApi.collapsedGroups?.has(group.name) ? 'mdi:chevron-right' : 'mdi:chevron-down'" class="op-group-chevron"></iconify-icon>
                    <span class="op-group-name">{{ group.name }}</span>
                    <span class="op-group-count">{{ group.operations.filter(op => filterModalApi.selectedOperations.has(op.id)).length }}/{{ group.operations.length }}</span>
                  </div>
                  <div v-if="!filterModalApi.collapsedGroups?.has(group.name)" class="op-group-body">
                    <div v-for="op in group.operations" :key="op.id" class="operation-item" :class="{ selected: filterModalApi.selectedOperations.has(op.id) }" @click="toggleOperationSelection(filterModalApi, op.id)">
                      <input type="checkbox" class="operation-checkbox" :checked="filterModalApi.selectedOperations.has(op.id)" @click.stop="toggleOperationSelection(filterModalApi, op.id)" />
                      <div class="operation-content">
                        <div class="operation-header"><span class="method-badge" :class="op.method.toLowerCase()">{{ op.method }}</span><span class="operation-id">{{ op.id }}</span></div>
                        <div class="operation-path">{{ op.path }}</div>
                        <div v-if="op.summary" class="operation-summary">{{ op.summary }}</div>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
              <div class="filter-live-preview">
                <iconify-icon icon="mdi:check-circle"></iconify-icon>
                <span>Exposing <strong>{{ filterModalApi.selectedOperations.size }}</strong> of {{ filterModalApi.availableOperations.length }} operations</span>
              </div>
            </div>
            <div v-else style="padding:20px; text-align:center; color:var(--text-dim); font-size:13px;">
              No operations loaded. Try re-detecting the API first.
            </div>
          </div>
          <div class="modal-footer">
            <button v-if="filterModalApi.filterMode" class="ghost" style="color:var(--red);" @click="clearFilter(filterModalApi)">Clear Filter</button>
            <button class="ghost" @click="filterModalApiId = ''" style="margin-left:auto;">Close</button>
          </div>
        </div>
      </div>
    </div>
  `,
}).mount("#app");

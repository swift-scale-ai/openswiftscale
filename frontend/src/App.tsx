import { useCallback, useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import {
  Activity,
  ArrowLeftRight,
  BarChart3,
  Boxes,
  BookOpen,
  Cable,
  ChevronLeft,
  Check,
  Copy,
  DollarSign,
  HeartPulse,
  KeyRound,
  Layers3,
  LayoutDashboard,
  LockKeyhole,
  Menu,
  Pencil,
  Trash2,
  X,
  type LucideIcon
} from "lucide-react";
import { localeOptions, useI18n, type Locale, type MessageKey } from "./i18n";

type ViewID = "overview" | "connections" | "models" | "access" | "usage" | "health";
type RouteStrategy = "failover" | "load-balance" | "hybrid";

interface GatewayStatus {
  name: string;
  version: string;
  uptime_seconds: number;
  models: number;
  available_models: number;
  routes: number;
  providers: number;
  telemetry: boolean;
  prompt_logging: boolean;
  api_base_url: string;
}

interface Model {
  id: string;
  name: string;
  family: string;
  provider: string;
  upstream_model: string;
  capabilities: string[];
  priority: number;
  weight: number;
  available: boolean;
}

interface Provider {
  id: string;
  name: string;
  type: string;
  base_url: string;
  authentication: string;
  api_key_header: string;
  allow_insecure_http: boolean;
  official: boolean;
  model_category?: "open-source" | "commercial" | "";
  enabled: boolean;
  has_api_key: boolean;
  models: Model[];
}

interface RoutingRuleMember {
  model_id: string;
  priority: number;
  weight: number;
}

interface RoutingRule {
  id: string;
  name: string;
  enabled: boolean;
  members: RoutingRuleMember[];
}

interface ModelRoutePolicy {
  model_id: string;
  strategy: RouteStrategy;
  created_at?: string;
  updated_at?: string;
}

interface UsageItem {
  created_at: string;
  model: string;
  provider: string;
  status: number;
  prompt_tokens: number;
  completion_tokens: number;
  latency_ms: number;
  cost_usd: number;
}

interface UsagePayload {
  summary: {
    requests: number;
    prompt_tokens: number;
    completion_tokens: number;
    average_latency_ms: number;
    cost_usd: number;
  };
  recent: UsageItem[];
}

interface OperationalState {
  service: "checking" | "healthy" | "unavailable";
  database: "checking" | "ready" | "unavailable";
}

interface APIKeyRecord {
  id: string;
  user_id: string;
  name: string;
  prefix: string;
  enabled: boolean;
  last_used_at?: string;
  created_at: string;
}

interface APIUserRecord {
  id: string;
  name: string;
  email?: string;
  enabled: boolean;
  keys: APIKeyRecord[];
  created_at: string;
  updated_at: string;
}

interface AdminCredentials {
  username: string;
  password: string;
}

const viewMeta: Record<ViewID, { eyebrow: MessageKey; title: MessageKey; description: MessageKey }> = {
  overview: { eyebrow: "communityGateway", title: "gatewayOverview", description: "metaOverview" },
  connections: { eyebrow: "providerConfiguration", title: "connections", description: "metaConnections" },
  models: { eyebrow: "modelRouting", title: "routeRules", description: "metaRoutes" },
  access: { eyebrow: "apiAccess", title: "apiUsers", description: "metaAccess" },
  usage: { eyebrow: "localObservability", title: "usage", description: "metaUsage" },
  health: { eyebrow: "operations", title: "health", description: "metaHealth" }
};

const navigation: Array<{ id: ViewID; label: MessageKey; description: MessageKey; icon: LucideIcon }> = [
  { id: "overview", label: "overview", description: "navOverview", icon: LayoutDashboard },
  { id: "connections", label: "connections", description: "navConnections", icon: Cable },
  { id: "models", label: "routeRules", description: "navRoutes", icon: Boxes },
  { id: "access", label: "apiAccess", description: "navAccess", icon: KeyRound },
  { id: "usage", label: "usage", description: "navUsage", icon: BarChart3 },
  { id: "health", label: "health", description: "navHealth", icon: Activity }
];

const capabilityOptions = [
  { value: "chat", label: "capChat" },
  { value: "responses", label: "capResponses" },
  { value: "streaming", label: "capStreaming" },
  { value: "tools", label: "capTools" },
  { value: "reasoning", label: "capReasoning" },
  { value: "structured_output", label: "capStructured" },
  { value: "vision", label: "capVision" },
  { value: "embeddings", label: "capEmbeddings" }
] as const;

const modelFamilyLabels: Record<string, string> = {
  generic: "Generic",
  gpt: "GPT",
  claude: "Claude",
  deepseek: "DeepSeek",
  glm: "GLM",
  mimo: "MiMo",
  minimax: "MiniMax",
  hy: "Tencent HY",
  nemotron: "Nemotron",
  qwen: "Qwen",
  gemini: "Gemini",
  llama: "Llama",
  grok: "Grok",
  mistral: "Mistral",
  command: "Command"
};

const openSourceModelFamilies = new Set(["deepseek", "glm", "hy", "mimo", "minimax", "nemotron", "qwen", "llama", "mistral"]);
const modelFamiliesByCategory: Record<"open-source" | "commercial", string[]> = {
  "open-source": ["deepseek", "qwen", "glm", "minimax", "hy", "mimo", "nemotron", "llama", "mistral", "generic"],
  commercial: ["gpt", "claude", "gemini", "grok", "command", "generic"]
};

function modelBrandKey(model: Model): string {
  const signature = `${model.family} ${model.id} ${model.name} ${model.provider}`.toLowerCase();
  if (signature.includes("deepseek")) return "deepseek";
  if (signature.includes("claude") || signature.includes("anthropic")) return "claude";
  if (signature.includes("minimax")) return "minimax";
  if (signature.includes("mimo") || signature.includes("xiaomi")) return "mimo";
  if (signature.includes("nemotron") || signature.includes("nvidia")) return "nemotron";
  if (signature.includes("tencent") || /(^|\s)hy(?:\d|\s|$)/.test(signature)) return "hy";
  if (signature.includes("glm") || signature.includes("zai")) return "glm";
  if (signature.includes("qwen")) return "qwen";
  if (signature.includes("gemini")) return "gemini";
  if (signature.includes("llama")) return "llama";
  if (signature.includes("grok")) return "grok";
  if (signature.includes("mistral")) return "mistral";
  if (signature.includes("command") || signature.includes("cohere")) return "command";
  if (signature.includes("gpt") || signature.includes("openai")) return "gpt";
  return "generic";
}

function isOpenSourceConnection(provider: Provider): boolean {
  if (provider.model_category) return provider.model_category === "open-source";
  return Boolean(provider.models?.length) && provider.models.every((model) => openSourceModelFamilies.has(modelBrandKey(model)));
}

const number = (value: number | undefined) => new Intl.NumberFormat(document.documentElement.lang || "en").format(value || 0);
const money = (value: number | undefined) => `$${Number(value || 0).toFixed(2)}`;

function fallbackAPIBaseURL(): string {
  const configured = String(import.meta.env.VITE_PUBLIC_GATEWAY_URL || "").replace(/\/$/, "");
  if (configured) return `${configured}/v1`;
  const url = new URL(window.location.origin);
  // Port 5173 is the Vite console only. Never advertise it as the inference API.
  if (url.port === "5173") url.port = "8080";
  return `${url.origin}/v1`;
}

function initialView(): ViewID {
  const value = window.location.hash.slice(1) as ViewID;
  return value in viewMeta ? value : "overview";
}

function basicAuthorization({ username, password }: AdminCredentials): string {
  const bytes = new TextEncoder().encode(`${username}:${password}`);
  let value = "";
  bytes.forEach((byte) => { value += String.fromCharCode(byte); });
  return `Basic ${window.btoa(value)}`;
}

async function api<T>(path: string, credentials: AdminCredentials, options: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...options,
    headers: {
      Authorization: basicAuthorization(credentials),
      ...(options.body ? { "Content-Type": "application/json" } : {}),
      ...options.headers
    }
  });
  if (!response.ok) {
    let message = response.status === 401 ? "Incorrect administrator username or password" : `Request failed (${response.status})`;
    try {
      const payload = await response.json() as { error?: { message?: string } };
      message = payload.error?.message || message;
    } catch {
      // The status-based message is sufficient for non-JSON failures.
    }
    throw new Error(message);
  }
  return (response.status === 204 ? null : await response.json()) as T;
}

function formatDuration(value: number | undefined): string {
  let seconds = Math.max(0, Number(value || 0));
  const days = Math.floor(seconds / 86400);
  seconds %= 86400;
  const hours = Math.floor(seconds / 3600);
  seconds %= 3600;
  const minutes = Math.floor(seconds / 60);
  return days ? `${days}d ${hours}h` : hours ? `${hours}h ${minutes}m` : `${minutes}m ${Math.floor(seconds % 60)}s`;
}

export function App() {
  const { t, locale, setLocale } = useI18n();
  const [view, setView] = useState<ViewID>(initialView);
  const [collapsed, setCollapsed] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [credentials, setCredentials] = useState<AdminCredentials>(() => ({
    username: sessionStorage.getItem("openswiftscale_admin_username") || "admin",
    password: sessionStorage.getItem("openswiftscale_admin_password") || sessionStorage.getItem("openswiftscale_management_token") || ""
  }));
  const [showAuth, setShowAuth] = useState(() => !sessionStorage.getItem("openswiftscale_admin_password") && !sessionStorage.getItem("openswiftscale_management_token"));
  const [connected, setConnected] = useState(false);
  const [connectionError, setConnectionError] = useState("");
  const [status, setStatus] = useState<GatewayStatus | null>(null);
  const [models, setModels] = useState<Model[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [routingRules, setRoutingRules] = useState<RoutingRule[]>([]);
  const [modelRoutePolicies, setModelRoutePolicies] = useState<ModelRoutePolicy[]>([]);
  const [usage, setUsage] = useState<UsagePayload | null>(null);
  const [apiUsers, setAPIUsers] = useState<APIUserRecord[]>([]);
  const [editingProvider, setEditingProvider] = useState<Provider | null | undefined>(undefined);
  const [editingRule, setEditingRule] = useState<RoutingRule | null | undefined>(undefined);
  const [routeWizardModelID, setRouteWizardModelID] = useState<string | null | undefined>(undefined);
  const [operational, setOperational] = useState<OperationalState>({ service: "checking", database: "checking" });

  const loadOperational = useCallback(async () => {
    try {
      const [healthResponse, readyResponse] = await Promise.all([fetch("/healthz"), fetch("/readyz")]);
      const health = await healthResponse.json() as { status?: string };
      const ready = await readyResponse.json() as { database?: string };
      setOperational({
        service: healthResponse.ok && health.status === "ok" ? "healthy" : "unavailable",
        database: readyResponse.ok && ready.database === "ok" ? "ready" : "unavailable"
      });
    } catch {
      setOperational({ service: "unavailable", database: "unavailable" });
    }
  }, []);

  const load = useCallback(async () => {
    if (!credentials.username || !credentials.password) return;
    try {
      const [statusPayload, modelPayload, usagePayload, providerPayload, rulePayload, modelRoutePolicyPayload, apiUserPayload] = await Promise.all([
        api<GatewayStatus>("/api/admin/status", credentials),
        api<Model[]>("/api/admin/models", credentials),
        api<UsagePayload>("/api/admin/usage?limit=50", credentials),
        api<Provider[]>("/api/admin/providers", credentials),
        api<RoutingRule[]>("/api/admin/routing-rules", credentials),
        api<ModelRoutePolicy[]>("/api/admin/model-route-policies", credentials),
        api<APIUserRecord[]>("/api/admin/users", credentials)
      ]);
      setStatus(statusPayload);
      setModels(modelPayload);
      setUsage(usagePayload);
      setProviders(providerPayload);
      setRoutingRules(rulePayload || []);
      setModelRoutePolicies(modelRoutePolicyPayload || []);
      setAPIUsers(apiUserPayload || []);
      setConnected(true);
      setShowAuth(false);
      setConnectionError("");
    } catch (cause) {
      setConnected(false);
      setShowAuth(true);
      setConnectionError(cause instanceof Error ? cause.message : String(cause));
    }
  }, [credentials]);

  useEffect(() => { void load(); }, [load]);
  useEffect(() => { void loadOperational(); }, [loadOperational]);
  useEffect(() => {
    const onHashChange = () => setView(initialView());
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  const selectView = (next: ViewID) => {
    window.location.hash = next;
    setView(next);
    setSidebarOpen(false);
  };

  const connect = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const nextCredentials = {
      username: String(form.get("username") || "").trim(),
      password: String(form.get("password") || "")
    };
    sessionStorage.setItem("openswiftscale_admin_username", nextCredentials.username);
    sessionStorage.setItem("openswiftscale_admin_password", nextCredentials.password);
    sessionStorage.removeItem("openswiftscale_management_token");
    setCredentials(nextCredentials);
    setConnectionError("");
  };

  const signOut = () => {
    sessionStorage.removeItem("openswiftscale_admin_username");
    sessionStorage.removeItem("openswiftscale_admin_password");
    sessionStorage.removeItem("openswiftscale_management_token");
    setCredentials({ username: "admin", password: "" });
    setConnected(false);
    setConnectionError("");
    setStatus(null);
    setModels([]);
    setProviders([]);
    setRoutingRules([]);
    setModelRoutePolicies([]);
    setUsage(null);
    setAPIUsers([]);
    setShowAuth(true);
  };

  const removeProvider = async (provider: Provider) => {
    if (!window.confirm(`Delete custom connection “${provider.id}” and its models?`)) return;
    try {
      await api<null>(`/api/admin/providers/${encodeURIComponent(provider.id)}`, credentials, { method: "DELETE" });
      await load();
    } catch (cause) {
      window.alert(cause instanceof Error ? cause.message : String(cause));
    }
  };

  const removeRoutingRule = async (rule: RoutingRule) => {
    if (!window.confirm(`Delete routing rule “${rule.id}”?`)) return;
    try {
      await api<null>(`/api/admin/routing-rules/${encodeURIComponent(rule.id)}`, credentials, { method: "DELETE" });
      await load();
    } catch (cause) {
      window.alert(cause instanceof Error ? cause.message : String(cause));
    }
  };

  const removeModelRoutePolicy = async (policy: ModelRoutePolicy) => {
    if (!window.confirm(`Delete model route rule “${policy.model_id}”? Provider connections and model endpoints will be preserved.`)) return;
    try {
      await api<null>(`/api/admin/model-route-policies/${encodeURIComponent(policy.model_id)}`, credentials, { method: "DELETE" });
      await load();
    } catch (cause) {
      window.alert(cause instanceof Error ? cause.message : String(cause));
    }
  };

  const meta = viewMeta[view];

  return <>
    <div className={`admin-shell${collapsed ? " is-sidebar-collapsed" : ""}`}>
      <aside className={`admin-sidebar${sidebarOpen ? " is-open" : ""}`}>
        <div className="sidebar-head">
          <button className="brand brand-button" type="button" onClick={() => selectView("overview")} aria-label="OpenSwiftScale overview">
            <img src="/swiftscale-logo.svg" alt="" />
            <span className="brand-copy">
              <span className="brand-title">OpenSwiftScale</span>
              <span className="brand-meta"><span className="brand-subtitle">{t("communityGateway")}</span><small>{t("local")}</small></span>
            </span>
          </button>
          <button className="sidebar-toggle" type="button" onClick={() => setCollapsed((value) => !value)} aria-label={t(collapsed ? "expandSidebar" : "collapseSidebar")} title={t(collapsed ? "expandSidebar" : "collapseSidebar")}>
            <ChevronLeft aria-hidden="true" />
          </button>
        </div>
        <nav className="admin-nav" aria-label="Console navigation">
          {navigation.map((item) => <button key={item.id} className={`admin-nav-item${view === item.id ? " is-active" : ""}`} type="button" onClick={() => selectView(item.id)} title={t(item.label)}>
            <span className="icon"><item.icon aria-hidden="true" /></span>
            <span className="nav-copy"><span className="nav-label">{t(item.label)}</span><span className="nav-desc">{t(item.description)}</span></span>
          </button>)}
        </nav>
        <div className="admin-state">
          <label>{t("gatewayState")}</label>
          <div className="admin-state-value"><span>{t(connected ? "online" : connectionError ? "locked" : "notConnected")}</span><i className={`status-dot${connected ? " online" : connectionError ? " error" : ""}`} /></div>
          <small>{t("localFirst")}</small>
        </div>
      </aside>

      <main className="admin-main">
        <header className="admin-topbar">
          <button className="admin-mobile-menu" type="button" onClick={() => setSidebarOpen((value) => !value)} aria-label={t("openNavigation")}><Menu aria-hidden="true" /></button>
          <div><p className="admin-eyebrow">{t(meta.eyebrow)}</p><h1>{t(meta.title)}</h1></div>
          <div className="admin-topbar-actions"><span className="local-chip"><i />{t("selfHosted")}</span><label className="language-select"><span>{t("language")}</span><select value={locale} onChange={(event) => setLocale(event.target.value as Locale)} aria-label={t("language")}>{localeOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label><button className="admin-action" type="button" onClick={connected ? signOut : () => setShowAuth(true)}>{t(connected ? "signOut" : "signIn")}</button></div>
        </header>
        <p className="admin-copy">{t(meta.description)}</p>

        {view === "overview" && <Overview status={status} usage={usage} models={models} connected={connected} />}
        {view === "connections" && <Connections providers={providers} connected={connected} onAdd={() => setEditingProvider(null)} onEdit={setEditingProvider} onDelete={(provider) => void removeProvider(provider)} />}
        {view === "models" && <Models models={models} policies={modelRoutePolicies} rules={routingRules} connected={connected} onCreateRoute={() => setRouteWizardModelID(null)} onManageRoute={setRouteWizardModelID} onDeleteRoute={(policy) => void removeModelRoutePolicy(policy)} onAddRule={() => setEditingRule(null)} onEditRule={setEditingRule} onDeleteRule={(rule) => void removeRoutingRule(rule)} />}
        {view === "access" && <APIAccess users={apiUsers} credentials={credentials} connected={connected} onRefresh={load} />}
        {view === "usage" && <Usage usage={usage} onRefresh={() => void load()} />}
        {view === "health" && <Health operational={operational} status={status} onRefresh={() => void loadOperational()} />}
      </main>
    </div>
    {showAuth && <div className="dialog-backdrop auth-backdrop" role="presentation" onMouseDown={(event) => { if (connected && event.target === event.currentTarget) setShowAuth(false); }}>
      <section className="auth-dialog" role="dialog" aria-modal="true" aria-labelledby="auth-dialog-title" aria-describedby="auth-dialog-description">
        <div className="auth-dialog-header">
          <div className="auth-copy">
            <span className="auth-icon"><LockKeyhole aria-hidden="true" /></span>
            <div><p className="section-eyebrow">OpenSwiftScale Console</p><h2 id="auth-dialog-title">{t("adminSignIn")}</h2></div>
          </div>
          {connected && <button type="button" className="icon-button" onClick={() => setShowAuth(false)} aria-label="Close sign in"><X aria-hidden="true" /></button>}
        </div>
        <p id="auth-dialog-description" className="auth-description">{t("signInHelp")}</p>
        <form onSubmit={connect}>
          <label className="auth-field" htmlFor="username"><span>{t("username")}</span><input id="username" name="username" type="text" autoComplete="username" required defaultValue={credentials.username} /></label>
          <label className="auth-field" htmlFor="password"><span>{t("password")}</span><input id="password" name="password" type="password" autoComplete="current-password" required autoFocus defaultValue={credentials.password} /></label>
          {connectionError && <p className="form-error compact-error" role="alert">{connectionError}</p>}
          <button className="admin-action">{t("signIn")}</button>
        </form>
        <p className="auth-security"><LockKeyhole aria-hidden="true" /> {t("credentialsTab")}</p>
      </section>
    </div>}
    {editingProvider !== undefined && <ProviderDialog provider={editingProvider} credentials={credentials} onClose={() => setEditingProvider(undefined)} onSaved={async () => { setEditingProvider(undefined); await load(); }} />}
    {editingRule !== undefined && <RoutingRuleDialog rule={editingRule} models={models} credentials={credentials} onClose={() => setEditingRule(undefined)} onSaved={async () => { setEditingRule(undefined); await load(); }} />}
    {routeWizardModelID !== undefined && <ModelRouteWizard initialModelID={routeWizardModelID} models={models} providers={providers} credentials={credentials} onClose={() => setRouteWizardModelID(undefined)} onSaved={async () => { setRouteWizardModelID(undefined); await load(); }} />}
  </>;
}

type ExampleLanguage = "curl" | "python" | "go" | "node" | "java" | "rust";

const exampleLanguages: Array<{ id: ExampleLanguage; label: string }> = [
  { id: "curl", label: "cURL" }, { id: "python", label: "Python" }, { id: "go", label: "Go" },
  { id: "node", label: "Node.js" }, { id: "java", label: "Java" }, { id: "rust", label: "Rust" }
];

function requestExample(language: ExampleLanguage, baseURL: string, modelID: string): string {
  const endpoint = `${baseURL}/chat/completions`;
  const body = `{"model":"${modelID}","messages":[{"role":"user","content":"Hello"}]}`;
  if (language === "curl") return [
    `curl ${endpoint} \\`,
    `  -H "Authorization: Bearer <gateway-api-key>" \\`,
    `  -H "Content-Type: application/json" \\`,
    `  -d '${body}'`
  ].join("\n");
  if (language === "python") return `from openai import OpenAI\n\nclient = OpenAI(\n    base_url="${baseURL}",\n    api_key="<gateway-api-key>",\n)\n\nresponse = client.chat.completions.create(\n    model="${modelID}",\n    messages=[{"role": "user", "content": "Hello"}],\n)\nprint(response.choices[0].message.content)`;
  if (language === "go") return `package main\n\nimport (\n  "bytes"\n  "fmt"\n  "io"\n  "net/http"\n)\n\nfunc main() {\n  body := []byte(\`${body}\`)\n  req, _ := http.NewRequest("POST", "${endpoint}", bytes.NewReader(body))\n  req.Header.Set("Authorization", "Bearer <gateway-api-key>")\n  req.Header.Set("Content-Type", "application/json")\n  res, err := http.DefaultClient.Do(req)\n  if err != nil { panic(err) }\n  defer res.Body.Close()\n  output, _ := io.ReadAll(res.Body)\n  fmt.Println(string(output))\n}`;
  if (language === "node") return `const response = await fetch("${endpoint}", {\n  method: "POST",\n  headers: {\n    "Authorization": "Bearer <gateway-api-key>",\n    "Content-Type": "application/json"\n  },\n  body: JSON.stringify({\n    model: "${modelID}",\n    messages: [{ role: "user", content: "Hello" }]\n  })\n});\n\nconsole.log(await response.json());`;
  if (language === "java") return `import java.net.URI;\nimport java.net.http.*;\n\nvar client = HttpClient.newHttpClient();\nvar request = HttpRequest.newBuilder(URI.create("${endpoint}"))\n    .header("Authorization", "Bearer <gateway-api-key>")\n    .header("Content-Type", "application/json")\n    .POST(HttpRequest.BodyPublishers.ofString("${body.replace(/"/g, '\\"')}"))\n    .build();\nvar response = client.send(request, HttpResponse.BodyHandlers.ofString());\nSystem.out.println(response.body());`;
  if (language === "rust") return `use reqwest::Client;\nuse serde_json::json;\n\n#[tokio::main]\nasync fn main() -> Result<(), reqwest::Error> {\n    let response = Client::new()\n        .post("${endpoint}")\n        .bearer_auth("<gateway-api-key>")\n        .json(&json!({\n            "model": "${modelID}",\n            "messages": [{"role": "user", "content": "Hello"}]\n        }))\n        .send().await?;\n    println!("{}", response.text().await?);\n    Ok(())\n}`;
  return `curl ${endpoint} \\\n+  -H "Authorization: Bearer <gateway-api-key>" \\\n+  -H "Content-Type: application/json" \\\n+  -d '${body}'`;
}

function Overview({ status, usage, models, connected }: { status: GatewayStatus | null; usage: UsagePayload | null; models: Model[]; connected: boolean }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState<"url" | "example" | null>(null);
  const [exampleLanguage, setExampleLanguage] = useState<ExampleLanguage>("curl");
  const callableModels = useMemo(() => {
    const unique = new Map<string, Model>();
    models.filter((model) => model.available).forEach((model) => { if (!unique.has(model.id)) unique.set(model.id, model); });
    return Array.from(unique.values()).sort((left, right) => left.name.localeCompare(right.name));
  }, [models]);
  const [selectedModelID, setSelectedModelID] = useState("");
  useEffect(() => {
    if (!callableModels.length) setSelectedModelID("");
    else if (!callableModels.some((model) => model.id === selectedModelID)) setSelectedModelID(callableModels[0].id);
  }, [callableModels, selectedModelID]);
  const apiBaseUrl = status?.api_base_url || fallbackAPIBaseURL();
  const curlExample = `curl ${apiBaseUrl}/chat/completions \\\n+  -H "Authorization: Bearer <gateway-api-key>" \\\n+  -H "Content-Type: application/json" \\\n+  -d '{"model":"your-model-id","messages":[{"role":"user","content":"Hello"}]}'`;
  const selectedModel = callableModels.find((model) => model.id === selectedModelID) || callableModels[0];
  const codeExample = selectedModel ? requestExample(exampleLanguage, apiBaseUrl, selectedModel.id) : exampleLanguage === "curl" ? curlExample.replace(/\n\+/g, "\n") : requestExample(exampleLanguage, apiBaseUrl, "your-model-id");
  const copyValue = async (kind: "url" | "example", value: string) => {
    await navigator.clipboard.writeText(value);
    setCopied(kind);
    window.setTimeout(() => setCopied((current) => current === kind ? null : current), 1600);
  };
  return <section className="admin-view is-visible"><div className="admin-metric-grid">
    <Metric icon={<HeartPulse />} label={t("gateway")} value={t(connected ? "online" : "offline")} detail={status ? `${t("version")} ${status.version} · ${number(status.uptime_seconds)}s ${t("uptime")}` : t("notConnected")} tone="blue" valueClass={connected ? "ok" : ""} />
    <Metric icon={<Layers3 />} label={t("availableModels")} value={status ? number(status.available_models) : "—"} detail={status ? `${number(status.models)} ${t("models")} · ${number(status.routes)} ${t("routes")}` : `— ${t("configured")}`} tone="blue" />
    <Metric icon={<ArrowLeftRight />} label={t("totalRequests")} value={usage ? number(usage.summary.requests) : "—"} detail={`${usage ? number(usage.summary.prompt_tokens + usage.summary.completion_tokens) : "—"} ${t("tokens")}`} tone="green" />
    <Metric icon={<DollarSign />} label={t("estimatedCost")} value={usage ? money(usage.summary.cost_usd) : "—"} detail={`${usage ? Math.round(usage.summary.average_latency_ms || 0) : "—"} ms ${t("averageLatency")}`} tone="orange" />
  </div>
    <article className="api-access-card">
      <header>
        <div><p className="section-eyebrow">{t("developerAccess")}</p><h2>{t("gatewayApi")}</h2><span>{t("gatewayApiHelp")}</span></div>
        <a className="api-docs-link" href="https://github.com/swift-scale-ai/OpenSwiftScale/blob/main/docs/API.md" target="_blank" rel="noreferrer"><BookOpen aria-hidden="true" />{t("apiDocumentation")}</a>
      </header>
      <div className="api-access-grid">
        <section className="api-address-block"><label>{t("apiBaseUrl")}</label><div><code>{apiBaseUrl}</code><button type="button" onClick={() => void copyValue("url", apiBaseUrl)} aria-label={t("copyApiUrl")}>{copied === "url" ? <Check /> : <Copy />}<span>{t(copied === "url" ? "copied" : "copy")}</span></button></div><small>{t("apiBaseHelp")}</small></section>
        <section className="api-auth-block"><label>{t("authentication")}</label><code>Authorization: Bearer &lt;gateway-api-key&gt;</code><small>{t("gatewayKeyLocation")} <code>secrets/gateway_api_keys.txt</code></small></section>
        <section className="api-endpoints-block"><label>{t("availableEndpoints")}</label><div><code>GET /models</code><code>POST /chat/completions</code><code>POST /responses</code><code>POST /embeddings</code></div></section>
      </div>
      <section className="api-callable-models"><header><div><label>{t("callableModels")}</label><small>{t("callableModelsHelp")}</small></div><span>{callableModels.length}</span></header>{selectedModel ? <div className="callable-model-picker"><select value={selectedModel.id} onChange={(event) => setSelectedModelID(event.target.value)} aria-label={t("callableModels")}>{callableModels.map((model) => <option value={model.id} key={model.id}>{model.name} ({model.id})</option>)}</select><div><strong>{selectedModel.name}</strong><code>{selectedModel.id}</code><small>{t("requestModelHelp")}</small></div></div> : <p className="empty compact-empty">{t("noCallableModels")}</p>}</section>
      <section className="api-code-examples"><header><div><label>{t("codeExamples")}</label><small>{t("codeExamplesHelp")}</small></div><nav aria-label={t("codeExamples")}>{exampleLanguages.map((language) => <button type="button" className={exampleLanguage === language.id ? "is-active" : ""} onClick={() => setExampleLanguage(language.id)} key={language.id}>{language.label}</button>)}</nav></header><div className="api-example-code"><pre><code>{codeExample}</code></pre><button type="button" onClick={() => void copyValue("example", codeExample)}>{copied === "example" ? <Check /> : <Copy />}{t(copied === "example" ? "copied" : "copyCode")}</button></div></section>
    </article>
    <OverviewCharts usage={usage} />
  </section>;
}

function Metric({ icon, label, value, detail, tone, valueClass = "" }: { icon: ReactNode; label: string; value: string; detail: string; tone: string; valueClass?: string }) {
  return <article className="admin-metric"><span className={`metric-icon ${tone}`}>{icon}</span><div><p>{label}</p><strong className={valueClass}>{value}</strong><small>{detail}</small></div></article>;
}

function OverviewCharts({ usage }: { usage: UsagePayload | null }) {
  const { t, locale } = useI18n();
  const recent = usage?.recent || [];
  const activity = useMemo(() => {
    const hour = 60 * 60 * 1000;
    const end = Math.floor(Date.now() / hour) * hour;
    const buckets = Array.from({ length: 12 }, (_, index) => ({ start: end - (11 - index) * hour, count: 0 }));
    recent.forEach((item) => {
      const timestamp = new Date(item.created_at).getTime();
      const bucket = buckets.find((candidate) => timestamp >= candidate.start && timestamp < candidate.start + hour);
      if (bucket) bucket.count += 1;
    });
    return buckets.map((bucket) => ({ ...bucket, label: new Date(bucket.start).toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit" }) }));
  }, [recent, locale]);
  const modelUsage = useMemo(() => {
    const totals = new Map<string, number>();
    recent.forEach((item) => totals.set(item.model, (totals.get(item.model) || 0) + item.prompt_tokens + item.completion_tokens));
    return Array.from(totals.entries()).map(([label, value]) => ({ label, value })).sort((left, right) => right.value - left.value).slice(0, 5);
  }, [recent]);
  const providerUsage = useMemo(() => {
    const totals = new Map<string, number>();
    recent.forEach((item) => totals.set(item.provider, (totals.get(item.provider) || 0) + 1));
    return Array.from(totals.entries()).map(([label, value]) => ({ label, value })).sort((left, right) => right.value - left.value).slice(0, 5);
  }, [recent]);
  const latency = useMemo(() => [...recent].reverse().slice(-24).map((item) => Math.max(0, item.latency_ms)), [recent]);
  const activityMax = Math.max(1, ...activity.map((item) => item.count));
  const tokenMax = Math.max(1, ...modelUsage.map((item) => item.value));
  const providerMax = Math.max(1, ...providerUsage.map((item) => item.value));
  const latencyMax = Math.max(1, ...latency);
  const latencyPoints = latency.map((value, index) => {
    const x = latency.length < 2 ? 300 : 18 + index * (564 / (latency.length - 1));
    const y = 142 - (value / latencyMax) * 112;
    return `${x},${y}`;
  }).join(" ");

  return <section className="overview-visuals" aria-label={t("navOverview")}>
    <article className="overview-chart activity-chart">
      <header><div><p className="section-eyebrow">{t("traffic")}</p><h2>{t("requestActivity")}</h2><span>{t("requestActivityHelp")}</span></div><strong>{number(activity.reduce((sum, item) => sum + item.count, 0))}</strong></header>
      <div className="activity-bars" role="img" aria-label={t("hourlyActivity")}>{activity.map((item, index) => <div key={item.start} className="activity-column"><div className="activity-track"><span style={{ height: `${Math.max(item.count ? 8 : 2, (item.count / activityMax) * 100)}%` }} title={`${item.label}: ${item.count} ${t("requests")}`} /></div>{index % 2 === 0 && <small>{item.label}</small>}</div>)}</div>
      {!recent.length && <ChartEmpty message={t("noRequestsChart")} />}
    </article>

    <article className="overview-chart latency-chart">
      <header><div><p className="section-eyebrow">{t("performance")}</p><h2>{t("latencyTrend")}</h2><span>{t("latencyHelp")}</span></div><strong>{usage ? `${Math.round(usage.summary.average_latency_ms || 0)} ms` : "—"}</strong></header>
      <div className="latency-plot" role="img" aria-label={t("latencyAria")}><svg viewBox="0 0 600 160" preserveAspectRatio="none" aria-hidden="true"><line x1="18" y1="30" x2="582" y2="30" /><line x1="18" y1="86" x2="582" y2="86" /><line x1="18" y1="142" x2="582" y2="142" />{latencyPoints && <polyline points={latencyPoints} />}{latency.length > 0 && <circle cx={latency.length < 2 ? 300 : 582} cy={142 - (latency[latency.length - 1] / latencyMax) * 112} r="4" />}</svg><span className="latency-high">{number(latencyMax)} ms</span><span className="latency-low">0 ms</span></div>
      {!latency.length && <ChartEmpty message={t("noLatency")} />}
    </article>

    <article className="overview-chart distribution-chart">
      <header><div><p className="section-eyebrow">{t("consumption")}</p><h2>{t("tokensByModel")}</h2><span>{t("tokensHelp")}</span></div><strong>{number(modelUsage.reduce((sum, item) => sum + item.value, 0))}</strong></header>
      <RankedBars items={modelUsage} max={tokenMax} suffix={t("tokens")} />
      {!modelUsage.length && <ChartEmpty message={t("noTokens")} />}
    </article>

    <article className="overview-chart distribution-chart provider-chart">
      <header><div><p className="section-eyebrow">{t("routing")}</p><h2>{t("providerTraffic")}</h2><span>{t("providerTrafficHelp")}</span></div><strong>{number(providerUsage.reduce((sum, item) => sum + item.value, 0))}</strong></header>
      <RankedBars items={providerUsage} max={providerMax} suffix={t("requests")} />
      {!providerUsage.length && <ChartEmpty message={t("noProviderTraffic")} />}
    </article>
  </section>;
}

function RankedBars({ items, max, suffix }: { items: Array<{ label: string; value: number }>; max: number; suffix: string }) {
  return <div className="ranked-bars">{items.map((item, index) => <div className="ranked-bar" key={item.label}><div><span><i>{index + 1}</i>{item.label}</span><strong>{number(item.value)} {suffix}</strong></div><div className="ranked-track"><span style={{ width: `${(item.value / max) * 100}%` }} /></div></div>)}</div>;
}

function ChartEmpty({ message }: { message: string }) {
  return <div className="chart-empty"><BarChart3 aria-hidden="true" /><span>{message}</span></div>;
}

function APIAccess({ users, credentials, connected, onRefresh }: { users: APIUserRecord[]; credentials: AdminCredentials; connected: boolean; onRefresh: () => Promise<void> }) {
  const { t, formatDate } = useI18n();
  const [showCreate, setShowCreate] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [revealedKey, setRevealedKey] = useState<{ value: string; name: string } | null>(null);
  const [copied, setCopied] = useState(false);

  const createUser = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setBusy(true); setError("");
    try {
      await api<APIUserRecord>("/api/admin/users", credentials, { method: "POST", body: JSON.stringify({ name: data.get("name"), email: data.get("email") }) });
      setShowCreate(false);
      await onRefresh();
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { setBusy(false); }
  };
  const updateUser = async (user: APIUserRecord, enabled: boolean) => {
    setError("");
    try {
      await api<null>(`/api/admin/users/${encodeURIComponent(user.id)}`, credentials, { method: "PATCH", body: JSON.stringify({ name: user.name, email: user.email || "", enabled }) });
      await onRefresh();
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  };
  const deleteUser = async (user: APIUserRecord) => {
    if (!window.confirm(t("deleteUserConfirm"))) return;
    setError("");
    try {
      await api<null>(`/api/admin/users/${encodeURIComponent(user.id)}`, credentials, { method: "DELETE" });
      await onRefresh();
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  };
  const createKey = async (event: FormEvent<HTMLFormElement>, user: APIUserRecord) => {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    setBusy(true); setError("");
    try {
      const result = await api<{ api_key: string; key: APIKeyRecord }>(`/api/admin/users/${encodeURIComponent(user.id)}/keys`, credentials, { method: "POST", body: JSON.stringify({ name: data.get("name") }) });
      setRevealedKey({ value: result.api_key, name: result.key.name });
      form.reset();
      await onRefresh();
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
    finally { setBusy(false); }
  };
  const revokeKey = async (user: APIUserRecord, key: APIKeyRecord) => {
    if (!window.confirm(t("revokeKeyConfirm"))) return;
    setError("");
    try {
      await api<null>(`/api/admin/users/${encodeURIComponent(user.id)}/keys/${encodeURIComponent(key.id)}`, credentials, { method: "DELETE" });
      await onRefresh();
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  };
  const copySecret = async () => {
    if (!revealedKey) return;
    await navigator.clipboard.writeText(revealedKey.value);
    setCopied(true);
  };

  return <section className="admin-view admin-panel is-visible api-access-view">
    <PanelHead eyebrow={t("apiAccess")} title={t("apiUsers")} description={t("apiUsersHelp")} action={<button className="admin-action" type="button" disabled={!connected} onClick={() => setShowCreate((value) => !value)}>{t("addApiUser")}</button>} />
    {showCreate && <form className="api-user-create" onSubmit={(event) => void createUser(event)}><label>{t("userName")}<input name="name" required maxLength={100} placeholder={t("userNamePlaceholder")} /></label><label>{t("emailOptional")}<input name="email" type="email" maxLength={254} placeholder="developer@example.com" /></label><div><button className="panel-action" type="button" onClick={() => setShowCreate(false)}>{t("cancel")}</button><button className="admin-action" disabled={busy}>{t(busy ? "saving" : "createUser")}</button></div></form>}
    {error && <p className="form-error api-access-error" role="alert">{error}</p>}
    <div className="api-user-list">{users.map((user) => <article className={`api-user-card${user.enabled ? "" : " is-disabled"}`} key={user.id}>
      <header><div><span className="api-user-avatar">{user.name.slice(0, 1).toUpperCase()}</span><div><h3>{user.name}</h3><p>{user.email || user.id}</p></div></div><div><span className={`pill${user.enabled ? "" : " neutral"}`}>{t(user.enabled ? "enabled" : "disabled")}</span><button className="panel-action" type="button" onClick={() => void updateUser(user, !user.enabled)}>{t(user.enabled ? "disable" : "enable")}</button><button className="danger-action" type="button" onClick={() => void deleteUser(user)}>{t("delete")}</button></div></header>
      <section className="api-key-section"><div className="api-key-heading"><div><h4>{t("apiKeys")}</h4><p>{t("apiKeysHelp")}</p></div><span>{user.keys.length}</span></div>
        <div className="api-key-list">{user.keys.length ? user.keys.map((key) => <div className="api-key-row" key={key.id}><span className="key-icon"><KeyRound /></span><div><strong>{key.name}</strong><code>{key.prefix}••••••••</code></div><div><small>{t("created")}</small><span>{formatDate(key.created_at)}</span></div><div><small>{t("lastUsed")}</small><span>{key.last_used_at ? formatDate(key.last_used_at) : t("never")}</span></div><button type="button" onClick={() => void revokeKey(user, key)}>{t("revoke")}</button></div>) : <p className="empty api-key-empty">{t("noApiKeys")}</p>}</div>
        <form className="api-key-create" onSubmit={(event) => void createKey(event, user)}><label><span className="sr-only">{t("keyName")}</span><input name="name" required maxLength={100} placeholder={t("keyNamePlaceholder")} /></label><button className="panel-action" disabled={busy || !user.enabled}>{t("createApiKey")}</button></form>
      </section>
    </article>)}</div>
    {!users.length && <p className="empty api-users-empty">{t(connected ? "noApiUsers" : "signInAccess")}</p>}
    {revealedKey && <div className="dialog-backdrop" role="presentation"><section className="secret-dialog" role="dialog" aria-modal="true" aria-labelledby="new-api-key-title"><div className="dialog-title"><div><p className="section-eyebrow">{t("apiKeyCreated")}</p><h2 id="new-api-key-title">{revealedKey.name}</h2><p>{t("copyKeyNow")}</p></div><button className="icon-button" type="button" onClick={() => { setRevealedKey(null); setCopied(false); }} aria-label={t("close")}><X /></button></div><div className="revealed-secret"><code>{revealedKey.value}</code><button type="button" onClick={() => void copySecret()}>{copied ? <Check /> : <Copy />}{t(copied ? "copied" : "copy")}</button></div><p className="secret-warning">{t("keyShownOnce")}</p><div className="dialog-actions"><button className="admin-action" type="button" onClick={() => { setRevealedKey(null); setCopied(false); }}>{t("done")}</button></div></section></div>}
  </section>;
}

function Connections({ providers, connected, onAdd, onEdit, onDelete }: { providers: Provider[]; connected: boolean; onAdd: () => void; onEdit: (provider: Provider) => void; onDelete: (provider: Provider) => void }) {
  const { t } = useI18n();
  const categories = [
    {
      id: "commercial",
      title: t("commercialModels"),
      description: t("commercialHelp"),
      providers: providers.filter((provider) => !isOpenSourceConnection(provider))
    },
    {
      id: "open-source",
      title: t("openSourceModels"),
      description: t("openSourceHelp"),
      providers: providers.filter(isOpenSourceConnection)
    }
  ];
  const renderProviderCard = (provider: Provider) => <article key={provider.id} className={`provider-card ${provider.official ? "official" : "custom"}`}>
    <div className="provider-head"><div><h3>{provider.name}</h3><p>{provider.id} · {provider.type}</p></div><span className="tag">{t(provider.official ? "official" : "thirdParty")}</span></div>
    <p>{provider.base_url}</p><p>{provider.models?.length || 0} {t("models")} · <span className={provider.has_api_key ? "key-set" : "key-missing"}>{t(provider.has_api_key ? "keySaved" : "apiKeyNeeded")}</span></p>
    <footer>{!provider.official && <button className="delete" type="button" onClick={() => onDelete(provider)}>{t("delete")}</button>}<button type="button" onClick={() => onEdit(provider)}>{t("configure")}</button></footer>
  </article>;
  return <section className="admin-view admin-panel is-visible">
    <PanelHead eyebrow={t("providerConnections")} title={t("bringKeys")} description={t("connectionHelp")} action={<button className="admin-action" type="button" onClick={onAdd}>{t("addCustomEndpoint")}</button>} />
    {providers.length ? <div className="provider-categories">{categories.map((category) => <section className={`provider-category ${category.id}`} key={category.id}>
      <header><div><h3>{category.title}</h3><p>{category.description}</p></div><span>{category.providers.length} connection{category.providers.length === 1 ? "" : "s"}</span></header>
      <div className="endpoint-groups">
        {[{ id: "official", title: t("officialEndpoints"), description: t("officialHelp"), items: category.providers.filter((provider) => provider.official) }, { id: "third-party", title: t("thirdPartyEndpoints"), description: t("thirdPartyHelp"), items: category.providers.filter((provider) => !provider.official) }].map((group) => <section className="endpoint-group" key={group.id}>
          <header><div><h4>{group.title}</h4><p>{group.description}</p></div><span>{group.items.length}</span></header>
          <div className="provider-grid">{group.items.length ? group.items.map(renderProviderCard) : <p className="empty category-empty">{t("noGroupConnections")}</p>}</div>
        </section>)}
      </div>
    </section>)}</div> : <p className="empty">{t(connected ? "noConnections" : "signInProviders")}</p>}
  </section>;
}

function Models({ models, policies, rules, connected, onCreateRoute, onManageRoute, onDeleteRoute, onAddRule, onEditRule, onDeleteRule }: { models: Model[]; policies: ModelRoutePolicy[]; rules: RoutingRule[]; connected: boolean; onCreateRoute: () => void; onManageRoute: (modelID: string) => void; onDeleteRoute: (policy: ModelRoutePolicy) => void; onAddRule: () => void; onEditRule: (rule: RoutingRule) => void; onDeleteRule: (rule: RoutingRule) => void }) {
  const { t } = useI18n();
  const [selectedFamily, setSelectedFamily] = useState("all");
  const safeRules = Array.isArray(rules) ? rules : [];
  const policyByModelID = useMemo(() => new Map(policies.map((policy) => [policy.model_id, policy])), [policies]);
  const groups = useMemo(() => {
    const routesByModelID = new Map<string, Model[]>();
    models.forEach((model) => routesByModelID.set(model.id, [...(routesByModelID.get(model.id) || []), model]));
    return policies.map((policy) => [policy.model_id, routesByModelID.get(policy.model_id) || []] as [string, Model[]]).filter(([, routes]) => routes.length > 0);
  }, [models, policies]);
  const families = useMemo(() => {
    const counts = new Map<string, number>();
    groups.forEach(([, routes]) => {
      const family = modelBrandKey(routes[0]);
      counts.set(family, (counts.get(family) || 0) + 1);
    });
    return Array.from(counts.entries()).sort(([left], [right]) => (modelFamilyLabels[left] || left).localeCompare(modelFamilyLabels[right] || right));
  }, [groups]);
  useEffect(() => {
    if (selectedFamily !== "all" && !families.some(([family]) => family === selectedFamily)) setSelectedFamily("all");
  }, [families, selectedFamily]);
  const visibleGroups = selectedFamily === "all" ? groups : groups.filter(([, routes]) => modelBrandKey(routes[0]) === selectedFamily);
  const modelByID = useMemo(() => new Map(groups.map(([modelID, routes]) => [modelID, routes[0]])), [groups]);
  return <section className="admin-view admin-panel is-visible">
    <PanelHead eyebrow={t("modelRouting")} title={t("routeRules")} description={t("routeRulesHelp")} action={<button className="admin-action" type="button" onClick={onCreateRoute}>{t("createRouteRule")}</button>} />
    <section className="routing-intro">
      <div><strong>{t("howRoutingWorks")}</strong><p>{t("routingExplanation")}</p></div>
      <div className="routing-legend"><span><b>1</b> {t("matchModel")}</span><span><b>2</b> {t("selectPriority")}</span><span><b>3</b> {t("balanceFailover")}</span></div>
    </section>
    <details className="routing-rule-section alias-rules" open={safeRules.length > 0}>
      <summary><div><p className="section-eyebrow">{t("advanced")}</p><h3>{t("virtualAliases")}</h3><span>{t("aliasHelp")}</span></div><span>{safeRules.length}</span></summary>
      <div className="routing-rule-heading alias-actions"><span>{t("aliasInstruction")}</span><button className="panel-action" type="button" onClick={onAddRule}>{t("createAlias")}</button></div>
      <div className="routing-rule-list">{safeRules.length ? safeRules.map((rule) => <article className="routing-rule-card" key={rule.id}>
        <header><div><strong>{rule.name}</strong><span>{t("routeId")} · {rule.id}</span></div><div><span className={`state ${rule.enabled ? "on" : "off"}`}>{t(rule.enabled ? "enabled" : "disabled")}</span><button type="button" onClick={() => onEditRule(rule)}>{t("edit")}</button><button className="delete" type="button" onClick={() => onDeleteRule(rule)}>{t("delete")}</button></div></header>
        <div className="routing-rule-chain">{[...rule.members].sort((left, right) => left.priority - right.priority || left.model_id.localeCompare(right.model_id)).map((member, index) => {
          const memberModel = modelByID.get(member.model_id);
          return <div className="routing-rule-member" key={member.model_id}><span className={`chain-order${index === 0 ? " is-primary" : ""}`}>{index === 0 ? "✓" : index + 1}</span><div><strong>{memberModel?.name || member.model_id}</strong><small>{member.model_id}</small><span>Priority {member.priority} · Weight {member.weight}</span></div></div>;
        })}</div>
      </article>) : <p className="empty compact-empty">{t("noAliases")}</p>}</div>
    </details>
    {groups.length ? <div className="model-catalog-layout">
      <aside className="model-family-nav" aria-label="Model families">
        <div className="family-nav-title"><span>{t("routeFamilies")}</span><small>{groups.length} {t("rules")}</small></div>
        <button type="button" className={selectedFamily === "all" ? "is-active" : ""} onClick={() => setSelectedFamily("all")} aria-current={selectedFamily === "all" ? "page" : undefined}><span>{t("allRules")}</span><strong>{groups.length}</strong></button>
        {families.map(([family, count]) => <button type="button" key={family} className={selectedFamily === family ? "is-active" : ""} onClick={() => setSelectedFamily(family)} aria-current={selectedFamily === family ? "page" : undefined}><span>{modelFamilyLabels[family] || family}</span><strong>{count}</strong></button>)}
      </aside>
      <div className="model-family-content">
        <div className={`model-route-groups${selectedFamily === "all" ? " is-all-rules" : ""}`}>{visibleGroups.map(([modelID, routes]) => {
          const sortedRoutes = [...routes].sort((left, right) => left.priority - right.priority || right.weight - left.weight || left.provider.localeCompare(right.provider));
          const singleEndpoint = sortedRoutes.length === 1;
          const strategy = policyByModelID.get(modelID)?.strategy || "failover";
          return <article className="model-route-group" key={modelID}>
            <header>
              <div className="route-rule-identity"><span className="model-brand">{modelFamilyLabels[modelBrandKey(routes[0])] || modelBrandKey(routes[0])}</span><h3>{routes[0].name}</h3><code>({modelID})</code></div>
              <div className="route-rule-meta"><span className="route-strategy"><i aria-hidden="true" />{t(strategy === "load-balance" ? "loadBalance" : strategy)}</span><span className="route-endpoint-count">{sortedRoutes.length} {t(singleEndpoint ? "endpoint" : "endpoints")}</span></div>
              <div className="route-group-actions"><button className="edit" type="button" onClick={() => onManageRoute(modelID)}><Pencil aria-hidden="true" />{t("edit")}</button><button className="delete" type="button" onClick={() => { const policy = policyByModelID.get(modelID); if (policy) onDeleteRoute(policy); }}><Trash2 aria-hidden="true" />{t("delete")}</button></div>
            </header>
            <div className="route-list">{sortedRoutes.map((route, index) => <div className="model-route" key={`${route.id}:${route.provider}`}>
              <span className={`route-order${index === 0 ? " is-primary" : ""}`} aria-label={`${t("endpoint")} ${index + 1}`}>{index + 1}</span>
              <div className="route-identity"><strong>{route.provider}</strong>{route.upstream_model !== modelID && <span>{route.upstream_model}</span>}</div>
              <span className={`state ${route.available ? "on" : "off"}`}>{t(route.available ? "ready" : "noKey")}</span>
              <div className="route-policy">{singleEndpoint ? <span className="primary-route">{t("primaryEndpoint")}</span> : <><span>{t("priority")} <strong>{route.priority}</strong></span><span>{t("weight")} <strong>{route.weight}</strong></span></>}</div>
            </div>)}</div>
          </article>;
        })}</div>
      </div>
    </div> : <div className="empty route-empty">{connected ? <><strong>{t("noRouteRules")}</strong><span>{t("noRouteRulesHelp")}</span><button className="admin-action" type="button" onClick={onCreateRoute}>{t("createFirstRoute")}</button></> : t("signInRoutes")}</div>}
  </section>;
}

function Usage({ usage, onRefresh }: { usage: UsagePayload | null; onRefresh: () => void }) {
  const { t, formatDate } = useI18n();
  return <section className="admin-view admin-panel is-visible">
    <PanelHead eyebrow={t("localObservability")} title={t("recentRequests")} description={t("usageHelp")} action={<button className="panel-action" type="button" onClick={onRefresh}>{t("refresh")}</button>} />
    <div className="table-wrap"><table><thead><tr><th>{t("time")}</th><th>{t("model")}</th><th>{t("provider")}</th><th>{t("status")}</th><th>{t("tokens")}</th><th>{t("latency")}</th><th>{t("cost")}</th></tr></thead><tbody>{usage?.recent?.length ? usage.recent.map((item) => <tr key={`${item.created_at}-${item.model}`}><td>{formatDate(item.created_at)}</td><td>{item.model}</td><td>{item.provider}</td><td className={item.status < 400 ? "ok" : "bad"}>{item.status}</td><td>{number(item.prompt_tokens + item.completion_tokens)}</td><td>{number(item.latency_ms)} ms</td><td>{money(item.cost_usd)}</td></tr>) : <tr><td colSpan={7} className="empty">{t("noUsage")}</td></tr>}</tbody></table></div>
  </section>;
}

function Health({ operational, status, onRefresh }: { operational: OperationalState; status: GatewayStatus | null; onRefresh: () => void }) {
  const { t } = useI18n();
  const stateLabel = (value: OperationalState["service"] | OperationalState["database"]) => t(value === "ready" ? "readyState" : value === "healthy" ? "healthy" : value === "unavailable" ? "unavailable" : "checking");
  return <section className="admin-view admin-panel is-visible">
    <PanelHead eyebrow={t("operationalStatus")} title={t("gatewayHealth")} description={t("healthHelp")} action={<button className="panel-action" type="button" onClick={onRefresh}>{t("refresh")}</button>} />
    <div className="health-grid"><HealthCard label={t("httpService")} value={stateLabel(operational.service)} detail="/healthz" healthy={operational.service === "healthy" ? true : operational.service === "unavailable" ? false : undefined} /><HealthCard label={t("databaseReadiness")} value={stateLabel(operational.database)} detail="/readyz" healthy={operational.database === "ready" ? true : operational.database === "unavailable" ? false : undefined} /><HealthCard label={t("processUptime")} value={status ? formatDuration(status.uptime_seconds) : t("connectDetails")} detail={t("currentProcess")} /></div>
  </section>;
}

function HealthCard({ label, value, detail, healthy }: { label: string; value: string; detail: string; healthy?: boolean }) {
  return <article className="health-card"><span>{label}</span><strong className={healthy === true ? "ok" : healthy === false ? "bad" : ""}>{value}</strong><small>{detail}</small></article>;
}

function PanelHead({ eyebrow, title, description, action }: { eyebrow: string; title: string; description: string; action: ReactNode }) {
  return <div className="panel-head"><div><p className="section-eyebrow">{eyebrow}</p><h2>{title}</h2><p>{description}</p></div>{action}</div>;
}

interface NewRouteEndpoint {
  id: string;
  name: string;
  base_url: string;
  upstream_model: string;
  api_key: string;
  priority: number;
  weight: number;
  allow_insecure_http: boolean;
}

function ModelRouteWizard({ initialModelID, models, providers, credentials, onClose, onSaved }: { initialModelID: string | null; models: Model[]; providers: Provider[]; credentials: AdminCredentials; onClose: () => void; onSaved: () => Promise<void> }) {
  const { t } = useI18n();
  const modelOptions = useMemo(() => {
    const unique = new Map<string, Model>();
    models.forEach((model) => { if (!unique.has(model.id)) unique.set(model.id, model); });
    return Array.from(unique.values()).sort((left, right) => left.name.localeCompare(right.name));
  }, [models]);
  const [step, setStep] = useState(initialModelID ? 2 : 1);
  const [modelID, setModelID] = useState(initialModelID || modelOptions[0]?.id || "");
  const [strategy, setStrategy] = useState<RouteStrategy>("failover");
  const [policies, setPolicies] = useState<Record<string, { priority: number; weight: number }>>({});
  const [drafts, setDrafts] = useState<NewRouteEndpoint[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const routes = useMemo(() => models.filter((model) => model.id === modelID).sort((left, right) => left.priority - right.priority || left.provider.localeCompare(right.provider)), [modelID, models]);
  const canonical = routes[0] || modelOptions.find((model) => model.id === modelID);

  useEffect(() => {
    setPolicies(Object.fromEntries(routes.map((route) => [route.provider, { priority: route.priority || 100, weight: route.weight || 100 }])));
    const priorities = new Set(routes.map((route) => route.priority || 100));
    setStrategy(priorities.size <= 1 && routes.length > 1 ? "load-balance" : priorities.size < routes.length ? "hybrid" : "failover");
    setDrafts([]);
  }, [modelID, routes]);

  const endpointCount = routes.length + drafts.length;
  const applyStrategy = (next: RouteStrategy) => {
    setStrategy(next);
    setPolicies(Object.fromEntries(routes.map((route, index) => [route.provider, {
      priority: next === "load-balance" ? 10 : next === "hybrid" ? (index < 2 ? 10 : 20) : (index + 1) * 10,
      weight: 100
    }])));
    setDrafts((current) => current.map((draft, index) => ({
      ...draft,
      priority: next === "load-balance" ? 10 : next === "hybrid" ? (routes.length + index < 2 ? 10 : 20) : (routes.length + index + 1) * 10,
      weight: 100
    })));
  };
  const addEndpoint = () => {
    const index = drafts.length;
    setDrafts((current) => [...current, {
      id: "",
      name: "",
      base_url: "",
      upstream_model: canonical?.upstream_model || "",
      api_key: "",
      priority: strategy === "load-balance" ? 10 : strategy === "hybrid" ? (routes.length + index < 2 ? 10 : 20) : (routes.length + index + 1) * 10,
      weight: 100,
      allow_insecure_http: false
    }]);
  };
  const updateDraft = (index: number, patch: Partial<NewRouteEndpoint>) => setDrafts((current) => current.map((draft, draftIndex) => draftIndex === index ? { ...draft, ...patch } : draft));
  const validateEndpoints = () => {
    if (!endpointCount) return t("validationEndpoint");
    for (const draft of drafts) {
      if (!draft.id.trim() || !draft.name.trim() || !draft.base_url.trim() || !draft.upstream_model.trim()) return t("validationFields");
      if (!/^[a-z0-9][a-z0-9._-]{0,62}$/.test(draft.id.trim())) return t("validationConnectionId");
      if (draft.base_url.startsWith("http://") && !draft.allow_insecure_http) return t("validationHttp");
    }
    return "";
  };
  const next = () => {
    setError("");
    if (step === 1 && !modelID) return setError(t("validationModelId"));
    if (step === 3) {
      const message = validateEndpoints();
      if (message) return setError(message);
    }
    setStep((current) => Math.min(4, current + 1));
  };
  const save = async () => {
    const validationError = validateEndpoints();
    if (validationError) return setError(validationError);
    setBusy(true); setError("");
    try {
      for (const route of routes) {
        const provider = providers.find((candidate) => candidate.id === route.provider);
        if (!provider) continue;
        const policy = policies[route.provider] || { priority: route.priority, weight: route.weight };
        const payload: Record<string, unknown> = { id: provider.id, name: provider.name, enabled: provider.enabled };
        if (provider.official) {
          payload.routes = provider.models.map((providerModel) => ({
            model_id: providerModel.id,
            priority: providerModel.id === modelID ? policy.priority : providerModel.priority,
            weight: providerModel.id === modelID ? policy.weight : providerModel.weight
          }));
        } else {
          payload.type = provider.type;
          payload.base_url = provider.base_url;
          payload.authentication = provider.authentication;
          payload.api_key_header = provider.api_key_header;
          payload.allow_insecure_http = provider.allow_insecure_http || provider.base_url.startsWith("http://");
          payload.model = {
            id: route.id,
            name: route.name,
            family: route.family,
            upstream_model: route.upstream_model,
            capabilities: route.capabilities,
            priority: policy.priority,
            weight: policy.weight
          };
        }
        await api<Provider>("/api/admin/providers", credentials, { method: "POST", body: JSON.stringify(payload) });
      }
      for (const draft of drafts) {
        const payload: Record<string, unknown> = {
          id: draft.id.trim(),
          name: draft.name.trim(),
          type: "openai-compatible",
          base_url: draft.base_url.trim(),
          authentication: "bearer",
          api_key_header: "Authorization",
          allow_insecure_http: draft.allow_insecure_http,
          enabled: true,
          model: {
            id: modelID,
            name: canonical?.name || modelID,
            family: canonical?.family || "generic",
            upstream_model: draft.upstream_model.trim(),
            capabilities: canonical?.capabilities || capabilityOptions.map((capability) => capability.value),
            priority: draft.priority,
            weight: draft.weight
          }
        };
        if (draft.api_key.trim()) payload.api_key = draft.api_key.trim();
        await api<Provider>("/api/admin/providers", credentials, { method: "POST", body: JSON.stringify(payload) });
      }
      await api<ModelRoutePolicy>("/api/admin/model-route-policies", credentials, {
        method: "POST",
        body: JSON.stringify({ model_id: modelID, strategy })
      });
      await onSaved();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };
  const orderedPreview = [
    ...routes.map((route) => ({ id: route.provider, label: providers.find((provider) => provider.id === route.provider)?.name || route.provider, upstream: route.upstream_model, ...(policies[route.provider] || { priority: route.priority, weight: route.weight }), existing: true })),
    ...drafts.map((draft) => ({ id: draft.id, label: draft.name, upstream: draft.upstream_model, priority: draft.priority, weight: draft.weight, existing: false }))
  ].sort((left, right) => left.priority - right.priority || left.id.localeCompare(right.id));
  const stepLabels = [t("modelId"), t("strategy"), t("endpoints"), t("review")];

  return <div className="dialog-backdrop" role="presentation">
    <section className="provider-dialog route-wizard" role="dialog" aria-modal="true" aria-labelledby="route-wizard-title">
      <div className="dialog-title"><div><p className="section-eyebrow">{t("wizardTitle")}</p><h2 id="route-wizard-title">{initialModelID ? `${t("configure")} ${initialModelID}` : t("createModelRoute")}</h2><p>{t("wizardHelp")}</p></div><button type="button" className="icon-button" onClick={onClose} aria-label={t("close")}><X /></button></div>
      <ol className="wizard-steps">{stepLabels.map((label, index) => <li key={label} className={step === index + 1 ? "is-current" : step > index + 1 ? "is-complete" : ""}><span>{step > index + 1 ? "✓" : index + 1}</span><strong>{label}</strong></li>)}</ol>

      <div className="wizard-body">
        {step === 1 && <section className="wizard-panel"><p className="section-eyebrow">{t("step")} 1</p><h3>{t("selectPublicId")}</h3><p>{t("publicIdHelp")}</p><label>{t("publicModelId")}<select value={modelID} onChange={(event) => setModelID(event.target.value)}>{modelOptions.map((model) => <option key={model.id} value={model.id}>{model.name} · {model.id}</option>)}</select></label><div className="request-example"><span>{t("requestMatch")}</span><code>{`"model": "${modelID}"`}</code></div></section>}
        {step === 2 && <section className="wizard-panel"><p className="section-eyebrow">{t("step")} 2</p><h3>{t("chooseStrategy")}</h3><p>{t("strategyHelp")}</p><div className="strategy-grid">
          <button type="button" className={strategy === "failover" ? "is-selected" : ""} onClick={() => applyStrategy("failover")}><strong>{t("failover")}</strong><span>{t("failoverHelp")}</span></button>
          <button type="button" className={strategy === "load-balance" ? "is-selected" : ""} onClick={() => applyStrategy("load-balance")}><strong>{t("loadBalance")}</strong><span>{t("loadBalanceHelp")}</span></button>
          <button type="button" className={strategy === "hybrid" ? "is-selected" : ""} onClick={() => applyStrategy("hybrid")}><strong>{t("hybrid")}</strong><span>{t("hybridHelp")}</span></button>
        </div></section>}
        {step === 3 && <section className="wizard-panel"><div className="wizard-panel-heading"><div><p className="section-eyebrow">{t("step")} 3</p><h3>{t("configureEndpoints")}</h3><p>{t("endpointHelp")}</p></div><button className="panel-action" type="button" onClick={addEndpoint}>{t("addCompatibleEndpoint")}</button></div>
          <div className="wizard-endpoints">
            {routes.map((route, index) => { const provider = providers.find((candidate) => candidate.id === route.provider); const policy = policies[route.provider] || { priority: route.priority, weight: route.weight }; return <article key={route.provider}><span className="route-order">{index + 1}</span><div className="wizard-endpoint-name"><strong>{provider?.name || route.provider}</strong><span>{t(provider?.official ? "official" : "thirdParty")} · {route.upstream_model}</span></div><label>{t("priority")}<input type="number" min="1" max="10000" value={policy.priority} onChange={(event) => setPolicies((current) => ({ ...current, [route.provider]: { ...policy, priority: Number(event.target.value) } }))} /></label><label>{t("weight")}<input type="number" min="1" max="10000" value={policy.weight} onChange={(event) => setPolicies((current) => ({ ...current, [route.provider]: { ...policy, weight: Number(event.target.value) } }))} /></label></article>; })}
            {drafts.map((draft, index) => <article className="new-endpoint" key={index}><span className="route-order">{routes.length + index + 1}</span><div className="new-endpoint-fields"><label>{t("connectionId")}<input value={draft.id} placeholder="backup-provider" onChange={(event) => updateDraft(index, { id: event.target.value.toLowerCase() })} /></label><label>{t("displayName")}<input value={draft.name} placeholder="Backup provider" onChange={(event) => updateDraft(index, { name: event.target.value })} /></label><label className="wide">{t("baseUrl")}<input type="url" value={draft.base_url} placeholder="https://gateway.example.com/v1" onChange={(event) => updateDraft(index, { base_url: event.target.value })} /></label><label>{t("upstreamModelId")}<input value={draft.upstream_model} onChange={(event) => updateDraft(index, { upstream_model: event.target.value })} /></label><label>{t("apiKey")}<input type="password" value={draft.api_key} placeholder={t("optionalLocal")} onChange={(event) => updateDraft(index, { api_key: event.target.value })} /></label><label>{t("priority")}<input type="number" min="1" max="10000" value={draft.priority} onChange={(event) => updateDraft(index, { priority: Number(event.target.value) })} /></label><label>{t("weight")}<input type="number" min="1" max="10000" value={draft.weight} onChange={(event) => updateDraft(index, { weight: Number(event.target.value) })} /></label><label className="checkbox wide"><input type="checkbox" checked={draft.allow_insecure_http} onChange={(event) => updateDraft(index, { allow_insecure_http: event.target.checked })} />{t("allowLocalHttp")}</label></div><button className="remove-rule-member" type="button" onClick={() => setDrafts((current) => current.filter((_, draftIndex) => draftIndex !== index))}>{t("remove")}</button></article>)}
          </div>
        </section>}
        {step === 4 && <section className="wizard-panel"><p className="section-eyebrow">{t("step")} 4</p><h3>{t("reviewRoute")}</h3><p>{t("reviewHelp")} <code>{modelID}</code></p><div className="route-review"><header><div><span>{t("publicModelId")}</span><strong>{modelID}</strong></div><span>{t(strategy === "load-balance" ? "loadBalance" : strategy)}</span></header><div className="route-review-chain">{orderedPreview.map((endpoint, index) => <article key={`${endpoint.id}-${index}`}><span className={index === 0 ? "is-primary" : ""}>{index === 0 ? "✓" : index + 1}</span><div><strong>{endpoint.label || endpoint.id}</strong><small>{endpoint.upstream}</small><p>{t("priority")} {endpoint.priority} · {t("weight")} {endpoint.weight} · {t(endpoint.existing ? "existing" : "newEndpoint")}</p></div></article>)}</div></div><p className="wizard-note">{t("automaticFailover")}</p></section>}
      </div>
      <p className="form-error" role="alert">{error}</p>
      <div className="dialog-actions wizard-actions"><button type="button" className="panel-action" onClick={step === 1 ? onClose : () => { setError(""); setStep((current) => current - 1); }}>{t(step === 1 ? "cancel" : "back")}</button><div><button type="button" className="panel-action" onClick={onClose}>{t("cancel")}</button>{step < 4 ? <button type="button" className="admin-action" onClick={next}>{t("continue")}</button> : <button type="button" className="admin-action" disabled={busy} onClick={() => void save()}>{t(busy ? "saving" : "saveModelRoute")}</button>}</div></div>
    </section>
  </div>;
}

function RoutingRuleDialog({ rule, models, credentials, onClose, onSaved }: { rule: RoutingRule | null; models: Model[]; credentials: AdminCredentials; onClose: () => void; onSaved: () => Promise<void> }) {
  const { t } = useI18n();
  const modelOptions = useMemo(() => {
    const unique = new Map<string, Model>();
    models.forEach((model) => { if (!unique.has(model.id)) unique.set(model.id, model); });
    return Array.from(unique.values()).sort((left, right) => left.name.localeCompare(right.name));
  }, [models]);
  const [members, setMembers] = useState<RoutingRuleMember[]>(() => rule?.members?.length ? rule.members.map((member) => ({ ...member })) : modelOptions[0] ? [{ model_id: modelOptions[0].id, priority: 10, weight: 100 }] : []);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const updateMember = (index: number, patch: Partial<RoutingRuleMember>) => setMembers((current) => current.map((member, memberIndex) => memberIndex === index ? { ...member, ...patch } : member));
  const addMember = () => {
    const available = modelOptions.find((model) => !members.some((member) => member.model_id === model.id)) || modelOptions[0];
    if (!available) return;
    const nextPriority = members.length ? Math.max(...members.map((member) => member.priority)) + 10 : 10;
    setMembers((current) => [...current, { model_id: available.id, priority: nextPriority, weight: 100 }]);
  };
  const save = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setBusy(true); setError("");
    try {
      await api<RoutingRule>("/api/admin/routing-rules", credentials, { method: "POST", body: JSON.stringify({
        id: String(data.get("id") || "").trim(),
        name: String(data.get("name") || "").trim(),
        enabled: data.get("enabled") === "on",
        members
      }) });
      await onSaved();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  return <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <section className="provider-dialog routing-rule-dialog" role="dialog" aria-modal="true" aria-labelledby="routing-rule-dialog-title">
      <form onSubmit={(event) => void save(event)}>
        <div className="dialog-title"><div><p className="section-eyebrow">{t("advancedRouting")}</p><h2 id="routing-rule-dialog-title">{rule ? `${t("configure")} ${rule.name}` : t("createVirtualAlias")}</h2><p>{t("aliasDialogHelp")}</p></div><button type="button" className="icon-button" onClick={onClose} aria-label={t("close")}><X /></button></div>
        <div className="form-grid">
          <label>{t("aliasId")}<input name="id" required maxLength={63} pattern="[a-z0-9][a-z0-9._-]*" placeholder="smart-chat" defaultValue={rule?.id || ""} readOnly={Boolean(rule)} /></label>
          <label>{t("displayName")}<input name="name" required placeholder="Smart chat routing" defaultValue={rule?.name || ""} /></label>
          <label className="checkbox full"><input name="enabled" type="checkbox" defaultChecked={rule?.enabled ?? true} />{t("enableRule")}</label>
        </div>
        <section className="rule-member-editor">
          <div><div><strong>{t("modelsInRule")}</strong><p>{t("modelsInRuleHelp")}</p></div><button className="panel-action" type="button" onClick={addMember}>{t("addModel")}</button></div>
          <div className="rule-member-editor-list">{members.map((member, index) => <article key={`${index}-${member.model_id}`}>
            <span className="chain-order">{index + 1}</span>
            <label>{t("model")}<select value={member.model_id} onChange={(event) => updateMember(index, { model_id: event.target.value })}>{modelOptions.map((model) => <option key={model.id} value={model.id}>{model.name} · {model.id}</option>)}</select></label>
            <label>{t("priority")}<input type="number" min="1" max="10000" value={member.priority} onChange={(event) => updateMember(index, { priority: Number(event.target.value) })} /></label>
            <label>{t("weight")}<input type="number" min="1" max="10000" value={member.weight} onChange={(event) => updateMember(index, { weight: Number(event.target.value) })} /></label>
            <button className="remove-rule-member" type="button" onClick={() => setMembers((current) => current.filter((_, memberIndex) => memberIndex !== index))}>{t("remove")}</button>
          </article>)}</div>
        </section>
        <div className="dialog-actions"><button type="button" className="panel-action" onClick={onClose}>{t("cancel")}</button><button type="submit" className="admin-action" disabled={busy || members.length === 0}>{t(busy ? "saving" : "saveAlias")}</button></div>
        <p className="form-error" role="alert">{error}</p>
      </form>
    </section>
  </div>;
}

function ProviderDialog({ provider, credentials, onClose, onSaved }: { provider: Provider | null; credentials: AdminCredentials; onClose: () => void; onSaved: () => Promise<void> }) {
  const { t } = useI18n();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const official = Boolean(provider?.official);
  const model = provider?.models?.[0];
  const initialCategory: "open-source" | "commercial" = provider?.model_category === "commercial" ? "commercial" : provider?.model_category === "open-source" ? "open-source" : provider && !isOpenSourceConnection(provider) ? "commercial" : "open-source";
  const [modelCategory, setModelCategory] = useState<"open-source" | "commercial">(initialCategory);
  const initialFamily = model ? modelBrandKey(model) : modelFamiliesByCategory[initialCategory][0];
  const [modelFamily, setModelFamily] = useState(modelFamiliesByCategory[initialCategory].includes(initialFamily) ? initialFamily : modelFamiliesByCategory[initialCategory][0]);

  const changeModelCategory = (next: "open-source" | "commercial") => {
    setModelCategory(next);
    if (!modelFamiliesByCategory[next].includes(modelFamily)) setModelFamily(modelFamiliesByCategory[next][0]);
  };

  const save = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const apiKey = String(data.get("api_key") || "").trim();
    const payload: Record<string, unknown> = {
      id: String(data.get("id") || "").trim(),
      name: String(data.get("name") || "").trim(),
      enabled: data.get("enabled") === "on",
      clear_api_key: data.get("clear_api_key") === "on"
    };
    if (apiKey) payload.api_key = apiKey;
    if (official) {
      payload.routes = (provider?.models || []).map((route, index) => ({
        model_id: route.id,
        priority: Number(data.get(`route_priority_${index}`) || route.priority || 100),
        weight: Number(data.get(`route_weight_${index}`) || route.weight || 100)
      }));
    }
    if (!official) {
      payload.model_category = data.get("model_category") || "open-source";
      payload.type = data.get("type");
      payload.base_url = String(data.get("base_url") || "").trim();
      payload.authentication = data.get("authentication");
      payload.api_key_header = String(data.get("api_key_header") || "").trim();
      payload.allow_insecure_http = data.get("allow_insecure_http") === "on";
      const publicID = String(data.get("public_model_id") || "").trim();
      payload.model = {
        id: publicID,
        name: publicID,
        family: data.get("model_family"),
        upstream_model: String(data.get("upstream_model_id") || "").trim(),
        capabilities: data.getAll("capabilities").map(String),
        priority: Number(data.get("route_priority") || 100),
        weight: Number(data.get("route_weight") || 100)
      };
    }
    setBusy(true); setError("");
    try {
      await api<Provider>("/api/admin/providers", credentials, { method: "POST", body: JSON.stringify(payload) });
      await onSaved();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  };

  return <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <section className="provider-dialog" role="dialog" aria-modal="true" aria-labelledby="provider-dialog-title">
      <form onSubmit={(event) => void save(event)}>
        <div className="dialog-title"><div><p className="section-eyebrow">{t("modelConnection")}</p><h2 id="provider-dialog-title">{provider ? `${t("configure")} ${provider.name}` : t("addEndpoint")}</h2><p>{t("encryptedLocally")}</p></div><button type="button" className="icon-button" onClick={onClose} aria-label={t("close")}><X /></button></div>
        <div className="form-grid">
          {!official && <>
            <label>{t("modelType")}<select name="model_category" value={modelCategory} onChange={(event) => changeModelCategory(event.target.value as "open-source" | "commercial")}><option value="open-source">{t("openSourceCategory")}</option><option value="commercial">{t("commercialCategory")}</option></select><small>{t("modelTypeHelp")}</small></label>
            <label>{t("modelFamily")}<select name="model_family" value={modelFamily} onChange={(event) => setModelFamily(event.target.value)}>{modelFamiliesByCategory[modelCategory].map((family) => <option key={family} value={family}>{modelFamilyLabels[family] || family}</option>)}</select><small>{t(modelCategory === "commercial" ? "commercialCategory" : "openSourceCategory")}</small></label>
          </>}
          <label>{t("connectionId")}<input name="id" required maxLength={63} pattern="[a-z0-9][a-z0-9._-]*" placeholder="internal-gateway" defaultValue={provider?.id || ""} readOnly={Boolean(provider)} /></label>
          <label>{t("displayName")}<input name="name" required placeholder="Internal AI Gateway" defaultValue={provider?.name || ""} /></label>
          {official && <section className="system-model-routes full" aria-labelledby="system-model-routes-title">
            <div className="system-model-routes-head"><div><strong id="system-model-routes-title">{t("builtInModels")}</strong><p>{t("builtInHelp")}</p></div><span>{provider?.models?.length || 0} {t("models")}</span></div>
            <div className="system-model-route-list">{provider?.models?.map((route, index) => <article key={`${route.id}:${route.provider}`}>
              <header><div><span>{modelFamilyLabels[modelBrandKey(route)] || modelBrandKey(route)}</span><strong>{route.name}</strong></div><span className={`state ${route.available ? "on" : "off"}`}>{t(route.available ? "ready" : "noKey")}</span></header>
              <div className="system-route-fields">
                <label>{t("publicModelId")}<input value={route.id} readOnly aria-label={`${route.name} ${t("publicModelId")}`} /></label>
                <label>{t("upstreamModelId")}<input value={route.upstream_model} readOnly aria-label={`${route.name} ${t("upstreamModelId")}`} /></label>
                <label>{t("priority")}<input name={`route_priority_${index}`} type="number" min="1" max="10000" required defaultValue={route.priority || 100} /></label>
                <label>{t("weight")}<input name={`route_weight_${index}`} type="number" min="1" max="10000" required defaultValue={route.weight || 100} /></label>
              </div>
            </article>)}</div>
          </section>}
          {!official && <>
            <label>{t("protocol")}<select name="type" defaultValue={provider?.type || "openai-compatible"}><option value="openai-compatible">{t("openAICompatible")}</option><option value="anthropic">{t("anthropicMessages")}</option></select></label>
            <label>{t("authentication")}<select name="authentication" defaultValue={provider?.authentication || "bearer"}><option value="bearer">{t("bearerToken")}</option><option value="x-api-key">x-api-key</option><option value="api-key">api-key</option></select></label>
            <label className="full">{t("baseUrl")}<input name="base_url" type="url" placeholder="https://gateway.example.com" defaultValue={provider?.base_url || ""} /></label>
            <label>{t("apiKeyHeader")}<input name="api_key_header" placeholder="Authorization" defaultValue={provider?.api_key_header || ""} /></label>
            <label className="checkbox"><input name="allow_insecure_http" type="checkbox" defaultChecked={provider?.allow_insecure_http || provider?.base_url.startsWith("http://")} />{t("allowLocalHttp")}</label>
            <label>{t("publicModelId")}<input name="public_model_id" placeholder="my-model" defaultValue={model?.id || ""} /></label>
            <label>{t("upstreamModelId")}<input name="upstream_model_id" placeholder="provider-model-id" defaultValue={model?.upstream_model || ""} /></label>
            <label>{t("routePriority")}<input name="route_priority" type="number" min="1" max="10000" required defaultValue={model?.priority || 100} /><small>{t("lowerFirst")}</small></label>
            <label>{t("routeWeight")}<input name="route_weight" type="number" min="1" max="10000" required defaultValue={model?.weight || 100} /><small>{t("relativeShare")}</small></label>
            <fieldset className="capability-picker full">
              <legend>{t("capabilities")}</legend>
              <div>{capabilityOptions.map((capability) => <label className="capability-option" key={capability.value}><input name="capabilities" type="checkbox" value={capability.value} defaultChecked={!model || model.capabilities.includes(capability.value)} /><span>{t(capability.label)}</span></label>)}</div>
              <p>{t("capabilitiesHelp")}</p>
            </fieldset>
          </>}
          <label className="full">{t("providerApiKey")}<input name="api_key" type="password" autoComplete="new-password" placeholder={t("keepSavedKey")} /></label>
          <label className="checkbox full"><input name="enabled" type="checkbox" defaultChecked={provider?.enabled ?? true} />{t("enableConnection")}</label>
          {provider?.has_api_key && <label className="checkbox full"><input name="clear_api_key" type="checkbox" />{t("removeSavedKey")}</label>}
        </div>
        <p className="form-note">{t("saveDoesNotVerify")}</p>
        <div className="dialog-actions"><button type="button" className="panel-action" onClick={onClose}>{t("cancel")}</button><button type="submit" className="admin-action" disabled={busy}>{t(busy ? "saving" : "saveConnection")}</button></div>
        <p className="form-error" role="alert">{error}</p>
      </form>
    </section>
  </div>;
}

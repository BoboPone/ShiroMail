import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@/lib/auth-store";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  WorkspacePage,
  WorkspacePanel,
} from "@/components/layout/workspace-ui";
import { ChevronDown, ChevronRight, Loader2, Send } from "lucide-react";

type HttpMethod = "GET" | "POST" | "PUT" | "DELETE";

type ResponseState = {
  status: number;
  statusText: string;
  headers: Record<string, string>;
  body: string;
  duration: number;
} | null;

const PRESET_ENDPOINTS = [
  { method: "GET" as HttpMethod, path: "/api/v1/dashboard" },
  { method: "GET" as HttpMethod, path: "/api/v1/messages/search?q=test" },
  { method: "GET" as HttpMethod, path: "/api/v1/messages/trend?days=7" },
  { method: "GET" as HttpMethod, path: "/api/v1/mailbox-tags" },
];

const METHOD_COLORS: Record<HttpMethod, string> = {
  GET: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400 border-emerald-500/30",
  POST: "bg-blue-500/15 text-blue-700 dark:text-blue-400 border-blue-500/30",
  PUT: "bg-amber-500/15 text-amber-700 dark:text-amber-400 border-amber-500/30",
  DELETE: "bg-red-500/15 text-red-700 dark:text-red-400 border-red-500/30",
};

function getStatusColor(status: number): string {
  if (status >= 200 && status < 300) return "text-emerald-600 dark:text-emerald-400";
  if (status >= 400 && status < 500) return "text-amber-600 dark:text-amber-400";
  if (status >= 500) return "text-red-600 dark:text-red-400";
  return "text-muted-foreground";
}

export function ApiPlaygroundPage() {
  const { t } = useTranslation();
  const accessToken = useAuthStore((state) => state.accessToken);

  const [method, setMethod] = useState<HttpMethod>("GET");
  const [endpoint, setEndpoint] = useState("/api/v1/");
  const [requestBody, setRequestBody] = useState("");
  const [loading, setLoading] = useState(false);
  const [response, setResponse] = useState<ResponseState>(null);
  const [headersOpen, setHeadersOpen] = useState(false);

  const handleSend = useCallback(async () => {
    setLoading(true);
    setResponse(null);

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    if (accessToken) {
      headers["Authorization"] = `Bearer ${accessToken}`;
    }

    const startTime = performance.now();
    try {
      const fetchOptions: RequestInit = { method, headers };
      if ((method === "POST" || method === "PUT") && requestBody.trim()) {
        fetchOptions.body = requestBody;
      }

      const res = await fetch(endpoint, fetchOptions);
      const duration = Math.round(performance.now() - startTime);

      const responseHeaders: Record<string, string> = {};
      res.headers.forEach((value, key) => {
        responseHeaders[key] = value;
      });

      let body: string;
      const contentType = res.headers.get("content-type") || "";
      if (contentType.includes("application/json")) {
        const json = await res.json();
        body = JSON.stringify(json, null, 2);
      } else {
        body = await res.text();
      }

      setResponse({ status: res.status, statusText: res.statusText, headers: responseHeaders, body, duration });
    } catch (err) {
      const duration = Math.round(performance.now() - startTime);
      setResponse({
        status: 0,
        statusText: "Network Error",
        headers: {},
        body: err instanceof Error ? err.message : String(err),
        duration,
      });
    } finally {
      setLoading(false);
    }
  }, [method, endpoint, requestBody, accessToken]);

  const handlePreset = (preset: (typeof PRESET_ENDPOINTS)[number]) => {
    setMethod(preset.method);
    setEndpoint(preset.path);
    setRequestBody("");
  };

  return (
    <WorkspacePage>
      <WorkspacePanel
        title={t("apiPlayground.title")}
        description={t("apiPlayground.description")}
      >
        {/* Presets */}
        <div className="space-y-2">
          <p className="text-sm font-medium text-muted-foreground">{t("apiPlayground.preset")}</p>
          <div className="flex flex-wrap gap-2">
            {PRESET_ENDPOINTS.map((preset) => (
              <Button
                key={`${preset.method}-${preset.path}`}
                variant="outline"
                size="sm"
                className="font-mono text-xs"
                onClick={() => handlePreset(preset)}
              >
                <span className={`mr-1.5 font-semibold ${METHOD_COLORS[preset.method].split(" ").filter(c => c.startsWith("text-")).join(" ")}`}>
                  {preset.method}
                </span>
                {preset.path}
              </Button>
            ))}
          </div>
        </div>

        {/* Main split layout */}
        <div className="grid gap-4 lg:grid-cols-2">
          {/* Left panel: request config */}
          <div className="space-y-4">
            {/* Method selector + endpoint */}
            <div className="flex gap-2">
              <div className="flex rounded-md border border-border/60 overflow-hidden">
                {(["GET", "POST", "PUT", "DELETE"] as HttpMethod[]).map((m) => (
                  <button
                    key={m}
                    type="button"
                    onClick={() => setMethod(m)}
                    className={`px-3 py-1.5 text-xs font-semibold border-r last:border-r-0 transition-colors ${
                      method === m
                        ? METHOD_COLORS[m] + " bg-opacity-100"
                        : "text-muted-foreground hover:bg-muted/50"
                    }`}
                  >
                    {m}
                  </button>
                ))}
              </div>
              <Input
                className="flex-1 font-mono text-sm"
                value={endpoint}
                onChange={(e) => setEndpoint(e.target.value)}
                placeholder="/api/v1/..."
              />
            </div>

            {/* Headers display */}
            <div className="space-y-1.5">
              <p className="text-sm font-medium text-muted-foreground">{t("apiPlayground.headers")}</p>
              <div className="rounded-md border border-border/60 bg-muted/30 px-3 py-2 font-mono text-xs text-muted-foreground">
                <div>Authorization: Bearer {accessToken ? `${accessToken.slice(0, 12)}...` : "(none)"}</div>
                <div>Content-Type: application/json</div>
              </div>
            </div>

            {/* Request body */}
            {(method === "POST" || method === "PUT") && (
              <div className="space-y-1.5">
                <p className="text-sm font-medium text-muted-foreground">{t("apiPlayground.requestBody")}</p>
                <textarea
                  className="w-full min-h-[140px] rounded-md border border-border/60 bg-muted/20 px-3 py-2 font-mono text-sm resize-y focus:outline-none focus:ring-2 focus:ring-ring/40"
                  value={requestBody}
                  onChange={(e) => setRequestBody(e.target.value)}
                  placeholder='{ "key": "value" }'
                  spellCheck={false}
                />
              </div>
            )}

            {/* Send button */}
            <Button onClick={handleSend} disabled={loading || !endpoint.trim()} className="w-full">
              {loading ? (
                <Loader2 className="mr-2 size-4 animate-spin" />
              ) : (
                <Send className="mr-2 size-4" />
              )}
              {t("apiPlayground.send")}
            </Button>
          </div>

          {/* Right panel: response */}
          <div className="space-y-3">
            <p className="text-sm font-medium text-muted-foreground">{t("apiPlayground.response")}</p>

            {response ? (
              <div className="space-y-3 rounded-md border border-border/60 bg-muted/20 p-3">
                {/* Status + duration */}
                <div className="flex items-center justify-between">
                  <span className={`font-mono text-sm font-semibold ${getStatusColor(response.status)}`}>
                    {response.status} {response.statusText}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {t("apiPlayground.duration")}: {response.duration}ms
                  </span>
                </div>

                {/* Response headers (collapsible) */}
                <div>
                  <button
                    type="button"
                    onClick={() => setHeadersOpen(!headersOpen)}
                    className="flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
                  >
                    {headersOpen ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
                    {t("apiPlayground.headers")} ({Object.keys(response.headers).length})
                  </button>
                  {headersOpen && (
                    <div className="mt-2 rounded border border-border/40 bg-muted/30 px-3 py-2 font-mono text-xs max-h-[120px] overflow-y-auto">
                      {Object.entries(response.headers).map(([key, value]) => (
                        <div key={key} className="truncate">
                          <span className="text-muted-foreground">{key}:</span> {value}
                        </div>
                      ))}
                    </div>
                  )}
                </div>

                {/* Response body */}
                <pre className="rounded border border-border/40 bg-muted/30 px-3 py-2 font-mono text-xs max-h-[400px] overflow-auto whitespace-pre-wrap break-all">
                  <code>{response.body}</code>
                </pre>
              </div>
            ) : (
              <div className="flex items-center justify-center rounded-md border border-dashed border-border/60 bg-muted/10 px-4 py-16 text-sm text-muted-foreground">
                {t("apiPlayground.noResponse")}
              </div>
            )}
          </div>
        </div>
      </WorkspacePanel>
    </WorkspacePage>
  );
}

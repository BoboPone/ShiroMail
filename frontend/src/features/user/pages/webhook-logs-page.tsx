import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router-dom";
import { ArrowLeft, RotateCw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  WorkspaceEmpty,
  WorkspacePage,
  WorkspacePanel,
} from "@/components/layout/workspace-ui";
import { fetchWebhookDeliveries, fetchWebhooks, retryWebhookDelivery } from "../api";
import { showSuccess, showError } from "@/lib/toast";

export function WebhookLogsPage() {
  const { t } = useTranslation();
  const { id } = useParams<{ id: string }>();
  const webhookId = id ? Number(id) : null;
  const queryClient = useQueryClient();

  const webhooksQuery = useQuery({
    queryKey: ["portal-webhooks"],
    queryFn: fetchWebhooks,
  });

  const deliveriesQuery = useQuery({
    queryKey: ["webhook-deliveries", webhookId],
    queryFn: () => fetchWebhookDeliveries(webhookId!),
    enabled: webhookId !== null,
  });

  const retryMutation = useMutation({
    mutationFn: (deliveryId: number) => retryWebhookDelivery(deliveryId),
    onSuccess: (result) => {
      if (result.success) {
        showSuccess(t("webhookLogs.retrySuccess"));
      } else {
        showError(t("webhookLogs.retryFailed", { detail: result.errorMessage || `HTTP ${result.responseStatus}` }));
      }
      queryClient.invalidateQueries({ queryKey: ["webhook-deliveries", webhookId] });
    },
    onError: () => {
      showError(t("webhookLogs.retryFailed", { detail: "unknown error" }));
    },
  });

  const webhook = webhooksQuery.data?.find((w) => w.id === webhookId);
  const items = deliveriesQuery.data ?? [];

  return (
    <WorkspacePage>
      <div className="flex items-center gap-3">
        <Button asChild size="icon-sm" variant="ghost">
          <Link to="/dashboard/webhooks">
            <ArrowLeft className="size-4" />
          </Link>
        </Button>
        <div>
          <h1 className="text-lg font-semibold">{t("webhookLogs.title")}</h1>
          {webhook && (
            <p className="text-sm text-muted-foreground">{webhook.name} &mdash; {webhook.targetUrl}</p>
          )}
        </div>
      </div>

      <WorkspacePanel
        description={t("webhookLogs.description")}
        title={t("webhookLogs.panelTitle")}
      >
        {deliveriesQuery.isLoading ? (
          <div className="flex h-[120px] items-center justify-center text-sm text-muted-foreground">
            {t("common.loading")}
          </div>
        ) : items.length === 0 ? (
          <WorkspaceEmpty
            title={t("webhooks.deliveryLogsEmpty")}
            description={t("webhooks.deliveryLogsEmptyHint")}
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border/60 text-left text-muted-foreground">
                  <th className="px-3 py-2 font-medium">{t("webhookLogs.colTime")}</th>
                  <th className="px-3 py-2 font-medium">{t("webhookLogs.colEvent")}</th>
                  <th className="px-3 py-2 font-medium">{t("webhookLogs.colStatus")}</th>
                  <th className="px-3 py-2 font-medium">{t("webhookLogs.colLatency")}</th>
                  <th className="px-3 py-2 font-medium">{t("webhookLogs.colError")}</th>
                  <th className="px-3 py-2 font-medium">{t("webhookLogs.colActions")}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((log) => (
                  <tr key={log.id} className="border-b border-border/40 last:border-0">
                    <td className="whitespace-nowrap px-3 py-2.5 text-muted-foreground">
                      {new Date(log.createdAt).toLocaleString()}
                    </td>
                    <td className="px-3 py-2.5 font-medium">{log.event}</td>
                    <td className="px-3 py-2.5">
                      <Badge variant={log.success ? "default" : "destructive"}>
                        {log.responseStatus || "ERR"}
                      </Badge>
                    </td>
                    <td className="px-3 py-2.5 text-muted-foreground">{log.latencyMs}ms</td>
                    <td className="max-w-[200px] truncate px-3 py-2.5 text-xs text-destructive">
                      {log.errorMessage || "—"}
                    </td>
                    <td className="px-3 py-2.5">
                      {!log.success && (
                        <Button
                          size="icon-sm"
                          variant="ghost"
                          disabled={retryMutation.isPending}
                          onClick={() => retryMutation.mutate(log.id)}
                          title={t("webhookLogs.retry")}
                        >
                          <RotateCw className={`size-3.5 ${retryMutation.isPending ? "animate-spin" : ""}`} />
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </WorkspacePanel>
    </WorkspacePage>
  );
}

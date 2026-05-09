import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { WorkspaceEmpty } from "@/components/layout/workspace-ui";
import { fetchWebhookDeliveries } from "../api";

type Props = {
  webhookId: number | null;
  webhookName: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export function WebhookDeliveryLogsDialog({ webhookId, webhookName, open, onOpenChange }: Props) {
  const { t } = useTranslation();
  const { data, isLoading } = useQuery({
    queryKey: ["webhook-deliveries", webhookId],
    queryFn: () => fetchWebhookDeliveries(webhookId!),
    enabled: open && webhookId !== null,
  });

  const items = data ?? [];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t("webhooks.deliveryLogsTitle")}</DialogTitle>
          <DialogDescription>{webhookName}</DialogDescription>
        </DialogHeader>

        {isLoading ? (
          <div className="flex h-[100px] items-center justify-center text-sm text-muted-foreground">
            {t("common.loading")}
          </div>
        ) : items.length === 0 ? (
          <WorkspaceEmpty
            title={t("webhooks.deliveryLogsEmpty")}
            description={t("webhooks.deliveryLogsEmptyHint")}
          />
        ) : (
          <div className="space-y-2">
            {items.map((log) => (
              <div
                key={log.id}
                className="flex items-center gap-3 rounded-lg border border-border/60 px-3 py-2.5"
              >
                <Badge variant={log.success ? "default" : "destructive"} className="shrink-0">
                  {log.responseStatus || "ERR"}
                </Badge>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 text-sm">
                    <span className="font-medium">{log.event}</span>
                    <span className="text-muted-foreground">·</span>
                    <span className="text-muted-foreground">{log.latencyMs}ms</span>
                  </div>
                  {log.errorMessage && (
                    <p className="mt-0.5 truncate text-xs text-destructive">{log.errorMessage}</p>
                  )}
                </div>
                <span className="shrink-0 text-xs text-muted-foreground">
                  {new Date(log.createdAt).toLocaleString()}
                </span>
              </div>
            ))}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

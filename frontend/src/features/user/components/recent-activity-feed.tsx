import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { Mail, Paperclip } from "lucide-react";
import { WorkspaceEmpty } from "@/components/layout/workspace-ui";
import { fetchRecentActivity } from "../api";

export function RecentActivityFeed() {
  const { t } = useTranslation();
  const { data, isLoading } = useQuery({
    queryKey: ["dashboard-activity"],
    queryFn: () => fetchRecentActivity(8),
    staleTime: 30_000,
  });

  if (isLoading) {
    return (
      <div className="flex h-[120px] items-center justify-center text-sm text-muted-foreground">
        {t("common.loading")}
      </div>
    );
  }

  const items = data ?? [];
  if (items.length === 0) {
    return <WorkspaceEmpty title={t("dashboard.activityEmpty")} description={t("dashboard.activityEmptyHint")} />;
  }

  return (
    <div className="space-y-1">
      {items.map((item) => (
        <Link
          key={item.id}
          to={`/dashboard/mailboxes`}
          className="flex items-start gap-3 rounded-lg px-3 py-2.5 transition-colors hover:bg-muted/50"
        >
          <div className={`mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full ${item.isRead ? "bg-muted" : "bg-primary/10"}`}>
            <Mail className={`size-4 ${item.isRead ? "text-muted-foreground" : "text-primary"}`} />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className={`truncate text-sm ${item.isRead ? "" : "font-medium"}`}>
                {item.subject || t("dashboard.activityNoSubject")}
              </span>
              {item.hasAttachments && <Paperclip className="size-3 shrink-0 text-muted-foreground" />}
            </div>
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <span className="truncate">{item.fromAddr}</span>
              <span className="shrink-0">·</span>
              <span className="shrink-0">{formatRelativeTime(item.receivedAt)}</span>
            </div>
          </div>
        </Link>
      ))}
    </div>
  );
}

function formatRelativeTime(dateStr: string) {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMin = Math.floor(diffMs / 60000);

  if (diffMin < 1) return "刚刚";
  if (diffMin < 60) return `${diffMin}m`;
  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour}h`;
  const diffDay = Math.floor(diffHour / 24);
  return `${diffDay}d`;
}

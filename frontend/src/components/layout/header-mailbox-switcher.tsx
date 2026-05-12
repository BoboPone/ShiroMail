import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { fetchDashboard } from "@/features/user/api";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

/**
 * Quick mailbox switcher shown in the console header for user role.
 * Fetches the dashboard data from TanStack Query cache and navigates
 * to the selected mailbox on change.
 */
export function HeaderMailboxSwitcher() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data } = useQuery({
    queryKey: ["user-dashboard"],
    queryFn: fetchDashboard,
    staleTime: 60_000,
  });

  const mailboxes = (data?.mailboxes ?? []).filter((m) => m.status === "active");

  if (!mailboxes.length) return null;

  return (
    <Select
      onValueChange={(value) => {
        navigate(`/dashboard/mailboxes?id=${value}`);
      }}
    >
      <SelectTrigger
        aria-label={t("console.mailboxSwitcher")}
        className="max-w-[180px] text-xs"
        size="sm"
      >
        <SelectValue placeholder={t("console.mailboxSwitcherPlaceholder")} />
      </SelectTrigger>
      <SelectContent>
        {mailboxes.map((mailbox) => (
          <SelectItem key={mailbox.id} value={String(mailbox.id)}>
            <span className="truncate">{mailbox.address}</span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

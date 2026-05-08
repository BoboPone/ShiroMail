import i18n from "@/lib/i18n";

export function formatDateTime(value?: string) {
  if (!value) {
    return "—";
  }

  return new Intl.DateTimeFormat(i18n.language, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

export function formatCurrency(cents: number) {
  return new Intl.NumberFormat(i18n.language, {
    style: "currency",
    currency: "CNY",
  }).format(cents / 100);
}

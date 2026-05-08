import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronUp, Globe, Plus, RefreshCcw } from "lucide-react";
import i18n from "@/lib/i18n";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Checkbox } from "@/components/ui/checkbox";
import { DNSRecordTypeCombobox } from "@/components/ui/dns-record-type-combobox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { MultiOptionCombobox } from "@/components/ui/multi-option-combobox";
import { NoticeBanner } from "@/components/ui/notice-banner";
import { OptionCombobox, type OptionComboboxOption } from "@/components/ui/option-combobox";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import {
  WorkspaceEmpty,
  WorkspaceField,
  WorkspaceListRow,
  WorkspacePage,
  WorkspacePanel,
  WorkspaceBadge,
} from "@/components/layout/workspace-ui";
import { getAPIErrorMessage } from "@/lib/http";
import { formatDNSRecordValueForDisplay } from "@/lib/dns-record-display";
import { readPersistedState, writePersistedState } from "@/lib/persisted-state";
import { cn } from "@/lib/utils";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";
import {
  applyAdminDNSChangeSet,
  createAdminDomainProvider,
  deleteAdminDomainProvider,
  fetchAdminDomainProviderChangeSets,
  fetchAdminDomainProviderRecords,
  fetchAdminDomainProviderVerifications,
  fetchAdminDomainProviderZones,
  fetchAdminDomainProviders,
  fetchAdminDomains,
  generateAdminSubdomains,
  previewAdminDomainProviderChangeSet,
  upsertAdminDomain,
  updateAdminDomainProvider,
  validateAdminDomainProvider,
  type DNSChangeSetItem,
  type DomainProviderItem,
  type ProviderRecordItem,
  type VerificationProfileItem,
} from "../api";
import type { DomainOption } from "../../user/api";

function providerRecordMergeKey(record: ProviderRecordItem) {
  return [
    record.type.trim().toUpperCase(),
    record.name.trim().toLowerCase(),
    String(record.priority ?? 0),
  ].join("|");
}

function getProviderTypeByID(
  providers: DomainProviderItem[] | undefined,
  providerID: number | undefined,
) {
  if (!providerID) {
    return null;
  }
  return providers?.find((item) => item.id === providerID)?.provider ?? null;
}

function mergeProviderRecords(
  currentRecords: ProviderRecordItem[],
  nextRecords: ProviderRecordItem[],
) {
  const merged = [...currentRecords];
  const indexes = new Map<string, number>();

  merged.forEach((record, index) => {
    indexes.set(providerRecordMergeKey(record), index);
  });

  nextRecords.forEach((record) => {
    const key = providerRecordMergeKey(record);
    const existingIndex = indexes.get(key);
    if (existingIndex === undefined) {
      indexes.set(key, merged.length);
      merged.push(record);
      return;
    }
    merged[existingIndex] = record;
  });

  return merged;
}

type ProviderCredentials = {
  apiToken: string;
  apiEmail: string;
  apiKey: string;
  apiSecret: string;
};

const DEFAULT_PROVIDER_PERMISSIONS = ["zones.read", "dns.write"];
const PROVIDER_PERMISSION_OPTIONS: Record<"cloudflare" | "spaceship", OptionComboboxOption[]> = {
  cloudflare: [
    { value: "tokens.verify", label: "Token 验证", keywords: ["tokens.verify", "token verify"] },
    { value: "zones.read", label: "Zone 读取", keywords: ["zones.read", "zone read"] },
    { value: "dns.read", label: "DNS 读取", keywords: ["dns.read", "dns read"] },
    { value: "dns.write", label: "DNS 写入", keywords: ["dns.write", "dns write", "dns edit"] },
  ],
  spaceship: [
    { value: "zones.read", label: "Zone 读取", keywords: ["zones.read", "zone read"] },
    { value: "dns.read", label: "DNS 读取", keywords: ["dns.read", "dns read"] },
    { value: "dns.write", label: "DNS 写入", keywords: ["dns.write", "dns write"] },
  ],
};

function getProviderPermissionOptions(provider: string) {
  return PROVIDER_PERMISSION_OPTIONS[
    provider === "spaceship" ? "spaceship" : "cloudflare"
  ];
}

function sanitizeProviderPermissions(provider: string, permissions: string[]) {
  const supportedValues = new Set(
    getProviderPermissionOptions(provider).map((option) => option.value),
  );

  return permissions.filter((permission, index) => {
    if (!supportedValues.has(permission)) {
      return false;
    }
    return permissions.indexOf(permission) === index;
  });
}

type EditableProviderRecord = ProviderRecordItem & {
  localId: string;
};

type DnsWorkspaceTab =
  | "providers"
  | "zones"
  | "records";

function createEditableProviderRecord(
  record?: Partial<ProviderRecordItem>,
): EditableProviderRecord {
  return {
    localId: `record-${Math.random().toString(36).slice(2, 10)}`,
    id: record?.id,
    type: record?.type ?? "TXT",
    name: record?.name ?? "",
    value: record?.value ?? "",
    ttl: record?.ttl ?? 300,
    priority: record?.priority ?? 0,
    proxied: record?.proxied ?? false,
  };
}

function recordsToEditable(records: ProviderRecordItem[]) {
  return records.map((record) => createEditableProviderRecord(record));
}

function describeAdminProviderWorkspaceError(message: string) {
  const normalized = message.toLowerCase();
  if (normalized.includes("unsupported dns record type")) {
    return "当前工作区里包含暂不支持的记录类型，请先检查该 Zone 中的记录类型是否受支持。";
  }
  if (normalized.includes("invalid request headers")) {
    return "DNS 服务商拒绝了当前请求头，请检查鉴权方式是否与凭据匹配。";
  }
  if (normalized.includes("authentication") || normalized.includes("unauthorized") || normalized.includes("forbidden")) {
    return "DNS 服务商鉴权失败，请检查 API Token、邮箱、API Key 或 Secret 是否正确，并确认账号权限足够。";
  }
  if (normalized.includes("status 400")) {
    return "DNS 服务商拒绝了这次请求，请检查凭据格式、鉴权方式和接口权限是否正确。";
  }
  if (normalized.includes("status 401") || normalized.includes("status 403")) {
    return "DNS 服务商返回未授权，请检查 Provider 凭据是否过期或权限不足。";
  }
  if (normalized.includes("status 404")) {
    return "指定的 Zone 或记录在 DNS 服务商侧不存在，请确认域名已真正接入该 Provider。";
  }
  if (
    normalized.includes("status 429") ||
    normalized.includes("too many requests") ||
    normalized.includes("rate limit")
  ) {
    return "DNS 服务商当前触发了频率限制，请稍后再试，不要连续重复刷新。";
  }
  if (normalized.includes("status 5")) {
    return "DNS 服务商暂时不可用，请稍后再试。";
  }
  return message;
}

function editableToProviderRecords(records: EditableProviderRecord[]) {
  return records
    .map((record) => ({
      id: record.id,
      type: record.type.trim().toUpperCase(),
      name: record.name.trim(),
      value: record.value.trim(),
      ttl: Number.isFinite(record.ttl) ? record.ttl : 300,
      priority: Number.isFinite(record.priority) ? record.priority : 0,
      proxied: record.proxied,
    }))
    .filter((record) => record.type !== "" && record.name !== "" && record.value !== "");
}

function applyVerificationRepairRecords(
  currentRecords: EditableProviderRecord[],
  repairRecords: ProviderRecordItem[],
) {
  const merged = mergeProviderRecords(
    editableToProviderRecords(currentRecords),
    repairRecords.map((record) => ({ ...record })),
  );
  return recordsToEditable(merged);
}

function describeChangeSetOperations(changeSet: DNSChangeSetItem) {
  const counts = changeSet.operations.reduce(
    (summary, operation) => {
      if (operation.operation === "create") {
        summary.create += 1;
      } else if (operation.operation === "update") {
        summary.update += 1;
      } else if (operation.operation === "delete") {
        summary.delete += 1;
      } else {
        summary.other += 1;
      }
      return summary;
    },
    { create: 0, update: 0, delete: 0, other: 0 },
  );

  const items = [
    counts.create ? `${counts.create} create` : null,
    counts.update ? `${counts.update} update` : null,
    counts.delete ? `${counts.delete} delete` : null,
    counts.other ? `${counts.other} other` : null,
  ].filter(Boolean);

  return items.length ? items.join(" · ") : "无操作";
}

function formatChangeSetTimestamp(value?: string) {
  if (!value) {
    return "时间未知";
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat(i18n.language, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function restoreEditableRecordsFromChangeSet(
  baseRecords: ProviderRecordItem[],
  changeSet: DNSChangeSetItem,
) {
  let nextRecords = [...baseRecords];

  changeSet.operations.forEach((operation) => {
    if (operation.before) {
      const beforeKey = providerRecordMergeKey(operation.before);
      nextRecords = nextRecords.filter(
        (record) => providerRecordMergeKey(record) !== beforeKey,
      );
    }

    if (operation.after) {
      const afterKey = providerRecordMergeKey(operation.after);
      const existingIndex = nextRecords.findIndex(
        (record) => providerRecordMergeKey(record) === afterKey,
      );

      if (existingIndex === -1) {
        nextRecords.push(operation.after);
      } else {
        nextRecords[existingIndex] = operation.after;
      }
    }
  });

  return recordsToEditable(nextRecords);
}

function getProviderCredentialFields(provider: string, authType: string) {
  if (provider === "spaceship") {
    return [
      {
        key: "apiKey" as const,
        label: "API Key",
        placeholder: "输入 Spaceship API Key",
        type: "password",
      },
      {
        key: "apiSecret" as const,
        label: "API Secret",
        placeholder: "输入 Spaceship API Secret",
        type: "password",
      },
    ];
  }

  if (authType === "api_key") {
    return [
      {
        key: "apiEmail" as const,
        label: "Account Email",
        placeholder: "输入 Cloudflare 账号邮箱（仅 Global API Key 模式需要）",
        type: "email",
      },
      {
        key: "apiKey" as const,
        label: "Global API Key",
        placeholder: "输入 Cloudflare Global API Key",
        type: "password",
      },
    ];
  }

  return [
    {
      key: "apiToken" as const,
      label: "API Token",
      placeholder: "输入 Cloudflare API Token（推荐）",
      type: "password",
    },
  ];
}

function canSubmitProviderCredentials(
  provider: string,
  authType: string,
  credentials: ProviderCredentials,
  allowEmpty = false,
) {
  const hasAnyCredential =
    credentials.apiToken.trim() !== "" ||
    credentials.apiEmail.trim() !== "" ||
    credentials.apiKey.trim() !== "" ||
    credentials.apiSecret.trim() !== "";
  if (allowEmpty && !hasAnyCredential) {
    return true;
  }
  if (provider === "spaceship") {
    return credentials.apiKey.trim() !== "" && credentials.apiSecret.trim() !== "";
  }
  if (authType === "api_key") {
    return credentials.apiEmail.trim() !== "" && credentials.apiKey.trim() !== "";
  }
  return credentials.apiToken.trim() !== "";
}

function getProviderAuthModeMeta(provider: string, authType: string) {
  if (provider === "spaceship") {
    return {
      title: "Spaceship API Key + Secret",
      description: "当前模式下需要填写 API Key 与 API Secret，平台会用它们读取 Zone 与 DNS 记录。",
    };
  }

  if (authType === "api_key") {
    return {
      title: "Cloudflare Global API Key + Email",
      description: "当前模式下需要填写账号邮箱和 Global API Key，不再显示 API Token 输入框。",
    };
  }

  return {
    title: "Cloudflare API Token",
    description: "当前模式下只需要 API Token，推荐使用具备 Zone Read / DNS Read / DNS Edit 权限的 Token。",
  };
}

const ADMIN_PROVIDERS_PAGE_SIZE = 6;
const ADMIN_ZONES_PAGE_SIZE = 8;
const ADMIN_RECORDS_PAGE_SIZE = 10;
const ADMIN_CHANGESETS_PAGE_SIZE = 8;
const ADMIN_CHANGESET_EDITOR_PAGE_SIZE = 8;
const ADMIN_DOMAINS_CACHE_KEY = "shiro-email.admin-domains.cache";
const ADMIN_PROVIDERS_CACHE_KEY = "shiro-email.admin-domain-providers.cache";
const ADMIN_WORKSPACE_CACHE_KEY = "shiro-email.admin-domains.workspace";
const PERSISTED_QUERY_STALE_TIME = 60_000;
const PROVIDER_ZONE_FAILURE_COOLDOWN_MS = 45_000;

function parsePositiveIntParam(value: string | null) {
  if (!value) {
    return null;
  }
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    return null;
  }
  return parsed;
}

function paginateItems<T>(items: T[], page: number, pageSize: number) {
  const totalPages = Math.max(1, Math.ceil(items.length / pageSize));
  const safePage = Math.min(Math.max(page, 1), totalPages);
  const start = (safePage - 1) * pageSize;
  return {
    page: safePage,
    totalPages,
    items: items.slice(start, start + pageSize),
    total: items.length,
  };
}

function isProviderRateLimitedError(message: string) {
  const normalized = message.toLowerCase();
  return (
    normalized.includes("status 429") ||
    normalized.includes("too many requests") ||
    normalized.includes("rate limit")
  );
}

function PaginationControls({
  page,
  totalPages,
  total,
  pageSize,
  itemLabel,
  onPageChange,
}: {
  page: number;
  totalPages: number;
  total: number;
  pageSize: number;
  itemLabel: string;
  onPageChange: (page: number) => void;
}) {
  if (total <= pageSize) {
    return null;
  }

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border/60 bg-background/60 px-3 py-2">
      <p className="text-xs text-muted-foreground">
        第 {page} / {totalPages} 页 · 共 {total} 条{itemLabel}
      </p>
      <div className="flex items-center gap-2">
        <Button
          disabled={page <= 1}
          size="sm"
          type="button"
          variant="outline"
          onClick={() => onPageChange(page - 1)}
        >
          上一页
        </Button>
        <Button
          disabled={page >= totalPages}
          size="sm"
          type="button"
          variant="outline"
          onClick={() => onPageChange(page + 1)}
        >
          下一页
        </Button>
      </div>
    </div>
  );
}

function SectionToggle({
  expanded,
  title,
  description,
  meta,
  onToggle,
}: {
  expanded: boolean;
  title: string;
  description: string;
  meta?: ReactNode;
  onToggle: () => void;
}) {
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div className="space-y-1">
        <p className="text-sm font-medium">{title}</p>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {meta}
        <Button size="sm" type="button" variant="ghost" onClick={onToggle}>
          {expanded ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
          {expanded ? "收起" : "展开"}
        </Button>
      </div>
    </div>
  );
}

export function AdminDnsPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const persistedWorkspace = readPersistedState(ADMIN_WORKSPACE_CACHE_KEY, {
    activeTab: "providers" as DnsWorkspaceTab,
    providerZonePanel: null as {
      providerId: number;
      displayName: string;
      zones: Array<{ id: string; name: string; status: string }>;
    } | null,
    providerRecordPanel: null as {
      providerId: number;
      zoneId: string;
      zoneName: string;
      records: Array<{
        id?: string;
        type: string;
        name: string;
        value: string;
        ttl: number;
        priority: number;
        proxied: boolean;
      }>;
    } | null,
    changeSetHistory: [] as DNSChangeSetItem[],
    verificationProfiles: [] as VerificationProfileItem[],
    desiredRecordsDraft: [] as EditableProviderRecord[],
    changeSetPreview: null as DNSChangeSetItem | null,
    selectedChangeSetID: null as number | null,
  });
  const queryClient = useQueryClient();
  const emptyDomainDraft = {
    domain: "",
    status: "active",
    visibility: "private",
    publicationStatus: "draft",
    healthStatus: "unknown",
    providerAccountId: "",
    isDefault: false,
    weight: 100,
  };
  const [providerMutationError, setProviderMutationError] = useState<string | null>(null);
  const [domainMutationError, setDomainMutationError] = useState<string | null>(null);
  const [subdomainMutationError, setSubdomainMutationError] = useState<string | null>(null);
  const [providerDeleteError, setProviderDeleteError] = useState<string | null>(null);
  const [providerActionNotice, setProviderActionNotice] = useState<string | null>(null);
  const [providerValidationError, setProviderValidationError] = useState<string | null>(null);
  const [providerZoneError, setProviderZoneError] = useState<string | null>(null);
  const [zoneFailureCooldowns, setZoneFailureCooldowns] = useState<Record<string, number>>({});
  const [isCreateProviderDialogOpen, setCreateProviderDialogOpen] =
    useState(false);
  const [editingProviderId, setEditingProviderId] = useState<number | null>(null);
  const [editingProviderHasBoundDomains, setEditingProviderHasBoundDomains] = useState(false);
  const [isCreateDomainDialogOpen, setCreateDomainDialogOpen] = useState(false);
  const [editingDomainId, setEditingDomainId] = useState<number | null>(null);
  const [isGenerateSubdomainDialogOpen, setGenerateSubdomainDialogOpen] =
    useState(false);
  const [draft, setDraft] = useState(emptyDomainDraft);
  const [providerDraft, setProviderDraft] = useState({
    provider: "cloudflare",
    ownerType: "platform",
    displayName: "",
    authType: "api_token",
    status: "healthy",
    permissionValues: DEFAULT_PROVIDER_PERMISSIONS,
  });
  const [providerCredentials, setProviderCredentials] = useState<ProviderCredentials>({
    apiToken: "",
    apiEmail: "",
    apiKey: "",
    apiSecret: "",
  });
  const [selectedBaseDomainId, setSelectedBaseDomainId] = useState<number | "">(
    "",
  );
  const [prefixInput, setPrefixInput] = useState("mx\nmx.edge\nrelay.cn.hk");
  const [providerZonePanel, setProviderZonePanel] = useState<{
    providerId: number;
    displayName: string;
    zones: Array<{ id: string; name: string; status: string }>;
  } | null>(persistedWorkspace.providerZonePanel);
  const [providerRecordPanel, setProviderRecordPanel] = useState<{
    providerId: number;
    zoneId: string;
    zoneName: string;
    records: Array<{
      id?: string;
      type: string;
      name: string;
      value: string;
      ttl: number;
      priority: number;
      proxied: boolean;
    }>;
  } | null>(persistedWorkspace.providerRecordPanel);
  const [desiredRecordsDraft, setDesiredRecordsDraft] = useState<EditableProviderRecord[]>(
    persistedWorkspace.desiredRecordsDraft,
  );
  const [changeSetPreview, setChangeSetPreview] =
    useState<DNSChangeSetItem | null>(persistedWorkspace.changeSetPreview);
  const [changeSetHistory, setChangeSetHistory] = useState<DNSChangeSetItem[]>(
    persistedWorkspace.changeSetHistory,
  );
  const [selectedChangeSetID, setSelectedChangeSetID] = useState<number | null>(
    persistedWorkspace.selectedChangeSetID,
  );
  const [verificationProfiles, setVerificationProfiles] = useState<
    VerificationProfileItem[]
  >(persistedWorkspace.verificationProfiles);
  const [changeSetError, setChangeSetError] = useState<string | null>(null);
  const [changeSetNotice, setChangeSetNotice] = useState<string | null>(null);
  const [activeDnsTab, setActiveDnsTab] = useState<DnsWorkspaceTab>(
    persistedWorkspace.activeTab,
  );
  const [providerZonesExpanded, setProviderZonesExpanded] = useState(true);
  const [providerRecordsExpanded, setProviderRecordsExpanded] = useState(true);
  const [verificationExpanded, setVerificationExpanded] = useState(true);
  const [changeSetEditorExpanded, setChangeSetEditorExpanded] = useState(true);
  const [changeSetHistoryExpanded, setChangeSetHistoryExpanded] = useState(true);
  const [providerAccountsPage, setProviderAccountsPage] = useState(1);
  const [providerZonesPage, setProviderZonesPage] = useState(1);
  const [providerRecordsPage, setProviderRecordsPage] = useState(1);
  const [changeSetHistoryPage, setChangeSetHistoryPage] = useState(1);
  const [changeSetEditorPage, setChangeSetEditorPage] = useState(1);
  const [providerDeleteDialog, setProviderDeleteDialog] = useState<{
    id: number;
    name: string;
  } | null>(null);
  const isEditingDomain = editingDomainId !== null;

  const domainsQuery = useQuery({
    queryKey: ["admin-domains"],
    queryFn: fetchAdminDomains,
    staleTime: PERSISTED_QUERY_STALE_TIME,
    placeholderData: () => readPersistedState<DomainOption[]>(ADMIN_DOMAINS_CACHE_KEY, []),
  });
  const providersQuery = useQuery({
    queryKey: ["admin-domain-providers"],
    queryFn: fetchAdminDomainProviders,
    staleTime: PERSISTED_QUERY_STALE_TIME,
    placeholderData: () =>
      readPersistedState<Awaited<ReturnType<typeof fetchAdminDomainProviders>>>(
        ADMIN_PROVIDERS_CACHE_KEY,
        [],
      ),
  });
  const currentRecordProviderType = useMemo(
    () => getProviderTypeByID(providersQuery.data, providerRecordPanel?.providerId),
    [providerRecordPanel?.providerId, providersQuery.data],
  );
  const rootDomains = useMemo(
    () => (domainsQuery.data ?? []).filter((item) => item.kind === "root"),
    [domainsQuery.data],
  );
  const sortedChangeSetHistory = useMemo(
    () =>
      [...changeSetHistory].sort((left, right) => {
        const leftTime = Date.parse(left.appliedAt ?? left.createdAt ?? "");
        const rightTime = Date.parse(right.appliedAt ?? right.createdAt ?? "");

        return rightTime - leftTime;
      }),
    [changeSetHistory],
  );
  const verificationStatusSummary = useMemo(
    () =>
      verificationProfiles.reduce(
        (summary, profile) => {
          if (profile.status === "verified") {
            summary.verified += 1;
          } else if (profile.status === "drifted") {
            summary.drifted += 1;
          } else {
            summary.other += 1;
          }
          return summary;
        },
        { verified: 0, drifted: 0, other: 0 },
      ),
    [verificationProfiles],
  );
  const paginatedProviders = useMemo(
    () => paginateItems(providersQuery.data ?? [], providerAccountsPage, ADMIN_PROVIDERS_PAGE_SIZE),
    [providerAccountsPage, providersQuery.data],
  );
  const paginatedZones = useMemo(
    () => paginateItems(providerZonePanel?.zones ?? [], providerZonesPage, ADMIN_ZONES_PAGE_SIZE),
    [providerZonePanel?.zones, providerZonesPage],
  );
  const paginatedRecords = useMemo(
    () => paginateItems(providerRecordPanel?.records ?? [], providerRecordsPage, ADMIN_RECORDS_PAGE_SIZE),
    [providerRecordPanel?.records, providerRecordsPage],
  );
  const paginatedChangeSetHistory = useMemo(
    () => paginateItems(sortedChangeSetHistory, changeSetHistoryPage, ADMIN_CHANGESETS_PAGE_SIZE),
    [changeSetHistoryPage, sortedChangeSetHistory],
  );
  const paginatedChangeSetEditorRecords = useMemo(
    () => paginateItems(desiredRecordsDraft, changeSetEditorPage, ADMIN_CHANGESET_EDITOR_PAGE_SIZE),
    [changeSetEditorPage, desiredRecordsDraft],
  );
  const boundProviderIds = useMemo(
    () =>
      new Set(
        (domainsQuery.data ?? [])
          .map((domain) => domain.providerAccountId)
          .filter((providerAccountId): providerAccountId is number => typeof providerAccountId === "number"),
      ),
    [domainsQuery.data],
  );
  const isEditingProvider = editingProviderId !== null;
  const providerCoreFieldsLocked = isEditingProvider && editingProviderHasBoundDomains;
  const resetProviderForm = useCallback(() => {
    setEditingProviderId(null);
    setEditingProviderHasBoundDomains(false);
    setProviderDraft({
      provider: "cloudflare",
      ownerType: "platform",
      displayName: "",
      authType: "api_token",
      status: "healthy",
      permissionValues: DEFAULT_PROVIDER_PERMISSIONS,
    });
    setProviderCredentials({
      apiToken: "",
      apiEmail: "",
      apiKey: "",
      apiSecret: "",
    });
  }, []);
  const openCreateProviderDialog = useCallback(() => {
    setProviderActionNotice(null);
    setProviderMutationError(null);
    resetProviderForm();
    setCreateProviderDialogOpen(true);
  }, [resetProviderForm]);
  const openEditProviderDialog = useCallback((provider: DomainProviderItem) => {
    setProviderActionNotice(null);
    setProviderMutationError(null);
    setEditingProviderId(provider.id);
    setEditingProviderHasBoundDomains(boundProviderIds.has(provider.id));
    setProviderDraft({
      provider: provider.provider,
      ownerType: provider.ownerType || "platform",
      displayName: provider.displayName,
      authType: provider.authType,
      status: provider.status,
      permissionValues: sanitizeProviderPermissions(provider.provider, provider.capabilities),
    });
    setProviderCredentials({
      apiToken: "",
      apiEmail: "",
      apiKey: "",
      apiSecret: "",
    });
    setCreateProviderDialogOpen(true);
  }, [boundProviderIds]);
  useEffect(() => {
    writePersistedState(ADMIN_DOMAINS_CACHE_KEY, domainsQuery.data ?? []);
  }, [domainsQuery.data]);

  useEffect(() => {
    writePersistedState(ADMIN_PROVIDERS_CACHE_KEY, providersQuery.data ?? []);
  }, [providersQuery.data]);

  useEffect(() => {
    writePersistedState(ADMIN_WORKSPACE_CACHE_KEY, {
      activeTab: activeDnsTab,
      providerZonePanel,
      providerRecordPanel,
      changeSetHistory,
      verificationProfiles,
      desiredRecordsDraft,
      changeSetPreview,
      selectedChangeSetID,
    });
  }, [
    activeDnsTab,
    changeSetHistory,
    changeSetPreview,
    desiredRecordsDraft,
    providerRecordPanel,
    providerZonePanel,
    selectedChangeSetID,
    verificationProfiles,
  ]);

  async function refreshAdminDomainData() {
    setProviderActionNotice(null);
    await Promise.all([domainsQuery.refetch(), providersQuery.refetch()]);

    const providerIds = new Set((providersQuery.data ?? []).map((item) => item.id));

    if (providerZonePanel && providerIds.has(providerZonePanel.providerId)) {
      const zones = await fetchAdminDomainProviderZones(providerZonePanel.providerId);
      setProviderZonePanel({
        providerId: providerZonePanel.providerId,
        displayName: providerZonePanel.displayName,
        zones,
      });
    } else if (providerZonePanel) {
      resetChangeWorkspace();
    }

    if (providerRecordPanel && providerIds.has(providerRecordPanel.providerId)) {
      await refreshProviderRecordWorkspace({
        providerId: providerRecordPanel.providerId,
        zoneId: providerRecordPanel.zoneId,
        zoneName: providerRecordPanel.zoneName,
        preserveDesiredInput: true,
      });
    } else if (providerRecordPanel) {
      resetChangeWorkspace();
    }
  }
  const statusOptions = [
    { value: "active", label: "active" },
    { value: "paused", label: "paused" },
  ];
  const visibilityOptions = [
    { value: "private", label: "private" },
    { value: "public_pool", label: "public_pool" },
    { value: "platform_public", label: "platform_public" },
  ];
  const publicationOptions = [
    { value: "draft", label: "draft" },
    { value: "pending_review", label: "pending_review" },
    { value: "approved", label: "approved" },
    { value: "rejected", label: "rejected" },
  ];
  const providerOptions = (providersQuery.data ?? []).map((item) => ({
    value: String(item.id),
    label: item.displayName,
    keywords: [item.provider, item.ownerType, item.status],
  }));
  const requestedProviderId = parsePositiveIntParam(searchParams.get("providerId"));
  const requestedDomainId = parsePositiveIntParam(searchParams.get("domainId"));
  const requestedDomain = useMemo(
    () =>
      requestedDomainId !== null
        ? (domainsQuery.data ?? []).find((item) => item.id === requestedDomainId) ?? null
        : null,
    [domainsQuery.data, requestedDomainId],
  );
  const requestedProvider = useMemo(
    () =>
      requestedProviderId !== null
        ? (providersQuery.data ?? []).find((item) => item.id === requestedProviderId) ?? null
        : null,
    [providersQuery.data, requestedProviderId],
  );
  const autoWorkspaceRequestRef = useRef<string | null>(null);
  const pendingWorkspaceRefreshRef = useRef<number | null>(null);

  const clearInvalidSearchParams = useCallback((keys: Array<"providerId" | "domainId">) => {
    const nextParams = new URLSearchParams(searchParams);
    let changed = false;

    keys.forEach((key) => {
      if (nextParams.has(key)) {
        nextParams.delete(key);
        changed = true;
      }
    });

    if (!changed) {
      return;
    }

    navigate(
      {
        pathname: location.pathname,
        search: nextParams.toString() ? `?${nextParams.toString()}` : "",
      },
      { replace: true },
    );
  }, [location.pathname, navigate, searchParams]);

  const refreshProviderRecordWorkspace = useCallback(async (input: {
    providerId: number;
    zoneId: string;
    zoneName: string;
    preserveDesiredInput?: boolean;
    force?: boolean;
  }) => {
    const zoneKey = `${input.providerId}:${input.zoneId}`;
    const cooldownUntil = zoneFailureCooldowns[zoneKey] ?? 0;
    if (!input.force && cooldownUntil > Date.now()) {
      const waitSeconds = Math.max(1, Math.ceil((cooldownUntil - Date.now()) / 1000));
      throw new Error(`DNS 服务商当前仍在冷却中，请约 ${waitSeconds} 秒后再刷新此 Zone。`);
    }

    try {
      const [records, changeSets, verifications] = await Promise.all([
        fetchAdminDomainProviderRecords(input.providerId, input.zoneId),
        fetchAdminDomainProviderChangeSets(input.providerId, input.zoneId),
        fetchAdminDomainProviderVerifications(input.providerId, input.zoneId, input.zoneName),
      ]);

      setZoneFailureCooldowns((current) => {
        if (!(zoneKey in current)) {
          return current;
        }
        const next = { ...current };
        delete next[zoneKey];
        return next;
      });

      setProviderRecordPanel({
        providerId: input.providerId,
        zoneId: input.zoneId,
        zoneName: input.zoneName,
        records,
      });
      setChangeSetHistory(changeSets);
      setVerificationProfiles(verifications);

      if (!input.preserveDesiredInput) {
        setDesiredRecordsDraft(recordsToEditable(records));
      }
    } catch (error) {
      const message = getAPIErrorMessage(error, "载入 DNS 工作区失败");
      if (isProviderRateLimitedError(message)) {
        setZoneFailureCooldowns((current) => ({
          ...current,
          [zoneKey]: Date.now() + PROVIDER_ZONE_FAILURE_COOLDOWN_MS,
        }));
      }
      throw error;
    }
  }, [zoneFailureCooldowns]);

  function scheduleProviderWorkspaceRefresh(
    input: {
      providerId: number;
      zoneId: string;
      zoneName: string;
      preserveDesiredInput?: boolean;
      force?: boolean;
    },
    options?: {
      delayMs?: number;
      onErrorMessage?: string;
    },
  ) {
    if (pendingWorkspaceRefreshRef.current !== null) {
      window.clearTimeout(pendingWorkspaceRefreshRef.current);
    }

    pendingWorkspaceRefreshRef.current = window.setTimeout(() => {
      pendingWorkspaceRefreshRef.current = null;
      void refreshProviderRecordWorkspace(input).catch((error) => {
        setProviderZoneError(
          describeAdminProviderWorkspaceError(
            getAPIErrorMessage(
              error,
              options?.onErrorMessage ?? "重新拉取 DNS 记录失败。",
            ),
          ),
        );
      });
    }, options?.delayMs ?? 2500);
  }

  const resetChangeWorkspace = useCallback((options?: { keepZonePanel?: boolean }) => {
    if (!options?.keepZonePanel) {
      setProviderZonePanel(null);
      setActiveDnsTab("providers");
    } else {
      setActiveDnsTab("zones");
    }
    setProviderRecordPanel(null);
    setChangeSetHistory([]);
    setVerificationProfiles([]);
    setDesiredRecordsDraft([]);
    setChangeSetPreview(null);
    setSelectedChangeSetID(null);
    setChangeSetError(null);
    setChangeSetNotice(null);
  }, []);

  useEffect(() => {
    const providerIds = new Set((providersQuery.data ?? []).map((item) => item.id));

    if (providerZonePanel && !providerIds.has(providerZonePanel.providerId)) {
      resetChangeWorkspace();
      setProviderZoneError(null);
      setProviderValidationError(null);
      setProviderDeleteError(null);
      setProviderActionNotice(null);
    }
  }, [providerZonePanel, providersQuery.data, resetChangeWorkspace]);

  useEffect(() => {
    return () => {
      if (pendingWorkspaceRefreshRef.current !== null) {
        window.clearTimeout(pendingWorkspaceRefreshRef.current);
      }
    };
  }, []);

  async function resolveLiveAdminProvider(providerId: number) {
    const result = await providersQuery.refetch();
    const liveProviders = result.data ?? providersQuery.data ?? [];
    const provider = liveProviders.find((item) => item.id === providerId) ?? null;

    if (provider) {
      return provider;
    }

    queryClient.setQueryData<Awaited<ReturnType<typeof fetchAdminDomainProviders>>>(
      ["admin-domain-providers"],
      liveProviders,
    );

    if (providerZonePanel?.providerId === providerId || providerRecordPanel?.providerId === providerId) {
      resetChangeWorkspace();
    }

    setProviderActionNotice(null);
    setProviderZoneError(null);
    setProviderValidationError(null);
    setProviderDeleteError("Provider 账号已不存在，已自动刷新管理员 Provider 列表。");
    return null;
  }

  const upsertMutation = useMutation({
    mutationFn: upsertAdminDomain,
    onSuccess: async () => {
      setDomainMutationError(null);
      setDraft(emptyDomainDraft);
      setEditingDomainId(null);
      setCreateDomainDialogOpen(false);
      await queryClient.invalidateQueries({ queryKey: ["admin-domains"] });
      await queryClient.invalidateQueries({ queryKey: ["admin-overview"] });
    },
    onError: (error) => {
      setDomainMutationError(getAPIErrorMessage(error, "保存域名失败，请检查配置后重试。"));
    },
  });

  const generateMutation = useMutation({
    mutationFn: generateAdminSubdomains,
    onSuccess: async () => {
      setSubdomainMutationError(null);
      setGenerateSubdomainDialogOpen(false);
      await queryClient.invalidateQueries({ queryKey: ["admin-domains"] });
      await queryClient.invalidateQueries({ queryKey: ["admin-overview"] });
    },
    onError: (error) => {
      setSubdomainMutationError(getAPIErrorMessage(error, "批量生成子域名失败，请稍后重试。"));
    },
  });

  const createProviderMutation = useMutation({
    mutationFn: createAdminDomainProvider,
    onSuccess: async () => {
      setProviderMutationError(null);
      setProviderDeleteError(null);
      setProviderActionNotice("Provider 账号已添加。");
      resetProviderForm();
      setCreateProviderDialogOpen(false);
      await queryClient.invalidateQueries({
        queryKey: ["admin-domain-providers"],
      });
    },
    onError: (error) => {
      setProviderMutationError(getAPIErrorMessage(error, "新增 Provider 账号失败，请检查凭据或会话状态。"));
    },
  });
  const updateProviderMutation = useMutation({
    mutationFn: ({ providerAccountId, input }: { providerAccountId: number; input: Parameters<typeof updateAdminDomainProvider>[1] }) =>
      updateAdminDomainProvider(providerAccountId, input),
    onSuccess: async () => {
      setProviderMutationError(null);
      setProviderDeleteError(null);
      setProviderActionNotice("Provider 账号已更新。");
      resetProviderForm();
      setCreateProviderDialogOpen(false);
      await queryClient.invalidateQueries({
        queryKey: ["admin-domain-providers"],
      });
      await queryClient.invalidateQueries({ queryKey: ["admin-domains"] });
    },
    onError: (error) => {
      setProviderMutationError(getAPIErrorMessage(error, "更新 Provider 账号失败，请检查字段和绑定关系。"));
    },
  });

  const deleteProviderMutation = useMutation({
    mutationFn: deleteAdminDomainProvider,
    onSuccess: async (_, providerId) => {
      setProviderDeleteError(null);
      setProviderActionNotice("Provider 账号已删除。");
      queryClient.setQueryData<Awaited<ReturnType<typeof fetchAdminDomainProviders>>>(["admin-domain-providers"], (current) =>
        (current ?? []).filter((item) => item.id !== providerId),
      );
      if (providerZonePanel?.providerId === providerId) {
        resetChangeWorkspace();
      }
      await queryClient.invalidateQueries({
        queryKey: ["admin-domain-providers"],
      });
      await queryClient.invalidateQueries({ queryKey: ["admin-domains"] });
    },
    onError: (error) => {
      setProviderDeleteError(getAPIErrorMessage(error, "删除 Provider 账号失败，请先解除域名绑定。"));
    },
  });

  const validateProviderMutation = useMutation({
    mutationFn: validateAdminDomainProvider,
    onSuccess: async () => {
      setProviderValidationError(null);
      setProviderActionNotice("Provider 连接校验完成。");
      await queryClient.invalidateQueries({
        queryKey: ["admin-domain-providers"],
      });
    },
    onError: (error) => {
      setProviderValidationError(
        getAPIErrorMessage(error, "校验 Provider 账号失败，请检查鉴权方式与凭据。"),
      );
    },
  });

  const loadProviderZonesMutation = useMutation({
    mutationFn: async (providerAccount: {
      id: number;
      displayName: string;
    }) => {
      const zones = await fetchAdminDomainProviderZones(providerAccount.id);
      return {
        providerId: providerAccount.id,
        displayName: providerAccount.displayName,
        zones,
      };
    },
    onSuccess: (payload) => {
      setProviderZoneError(null);
      setProviderActionNotice(`已载入 ${payload.displayName} 的 Zone 列表。`);
      setProviderZonePanel(payload);
      setProviderZonesPage(1);
      setActiveDnsTab("zones");
      resetChangeWorkspace({ keepZonePanel: true });
    },
    onError: (error) => {
      setProviderZoneError(
        describeAdminProviderWorkspaceError(
          getAPIErrorMessage(error, "拉取 Provider Zones 失败，请检查连接与凭据。"),
        ),
      );
      resetChangeWorkspace();
    },
  });

  const loadProviderRecordsMutation = useMutation({
    mutationFn: async (input: {
      providerId: number;
      zoneId: string;
      zoneName: string;
    }) => {
      return input;
    },
    onSuccess: async (payload) => {
      try {
        await refreshProviderRecordWorkspace(payload);
        setChangeSetPreview(null);
        setChangeSetError(null);
        setChangeSetNotice(`已载入 ${payload.zoneName} 的 DNS Records。`);
        setProviderZoneError(null);
        setProviderRecordsPage(1);
        setChangeSetEditorPage(1);
        setChangeSetHistoryPage(1);
        setActiveDnsTab("records");
      } catch (error) {
        setProviderZoneError(
          describeAdminProviderWorkspaceError(
            getAPIErrorMessage(error, `载入 ${payload.zoneName} 的 DNS Records 失败。`),
          ),
        );
      }
    },
  });

  const previewChangeSetMutation = useMutation({
    mutationFn: async (input: {
      providerId: number;
      zoneId: string;
      zoneName: string;
      records: ProviderRecordItem[];
    }) => {
      return previewAdminDomainProviderChangeSet(
        input.providerId,
        input.zoneId,
        {
          zoneName: input.zoneName,
          records: input.records,
        },
      );
    },
    onSuccess: (payload) => {
      setChangeSetPreview(payload);
      setSelectedChangeSetID(payload.id);
      setChangeSetHistory((current) => [
        payload,
        ...current.filter((item) => item.id !== payload.id),
      ]);
      setChangeSetError(null);
      setChangeSetNotice(`Change Set 已生成：${payload.summary}`);
      setActiveDnsTab("records");
    },
    onError: (error) => {
      const message =
        describeAdminProviderWorkspaceError(
          getAPIErrorMessage(error, "预览自动配置失败"),
        );
      setChangeSetError(message);
    },
  });

  const applyChangeSetMutation = useMutation({
    mutationFn: applyAdminDNSChangeSet,
    onSuccess: (payload) => {
      setChangeSetPreview(payload);
      setSelectedChangeSetID(payload.id);
      setChangeSetHistory((current) => [
        payload,
        ...current.filter((item) => item.id !== payload.id),
      ]);
      setChangeSetError(null);
      setChangeSetNotice(payload.appliedAt ? "Change Set 已应用到上游 DNS。" : "Change Set 已更新。");
      setActiveDnsTab("records");
      if (providerRecordPanel) {
        scheduleProviderWorkspaceRefresh(
          {
            providerId: providerRecordPanel.providerId,
            zoneId: providerRecordPanel.zoneId,
            zoneName: providerRecordPanel.zoneName,
            preserveDesiredInput: true,
          },
          { onErrorMessage: "变更已应用，但重新拉取 DNS 记录失败。" },
        );
      }
    },
    onError: (error) => {
      setChangeSetError(
        describeAdminProviderWorkspaceError(
          getAPIErrorMessage(error, "应用自动配置失败"),
        ),
      );
    },
  });

  const saveProviderRecordsMutation = useMutation({
    mutationFn: async (input: {
      providerId: number;
      zoneId: string;
      zoneName: string;
      records: ReturnType<typeof editableToProviderRecords>;
    }) => {
      const preview = await previewAdminDomainProviderChangeSet(
        input.providerId,
        input.zoneId,
        {
          zoneName: input.zoneName,
          records: input.records,
        },
      );
      return applyAdminDNSChangeSet(preview.id);
    },
    onSuccess: async (payload, variables) => {
      const verifications = await fetchAdminDomainProviderVerifications(
        variables.providerId,
        variables.zoneId,
        variables.zoneName,
      );
      setChangeSetPreview(payload);
      setSelectedChangeSetID(payload.id);
      setChangeSetHistory((current) => [
        payload,
        ...current.filter((item) => item.id !== payload.id),
      ]);
      setChangeSetError(null);
      setChangeSetNotice("已保存并同步到 DNS 服务商。");
      setActiveDnsTab("records");
      if (providerRecordPanel) {
        setProviderRecordPanel({
          providerId: variables.providerId,
          zoneId: variables.zoneId,
          zoneName: variables.zoneName,
          records: variables.records,
        });
        setVerificationProfiles(verifications);
        setDesiredRecordsDraft(recordsToEditable(variables.records));
        scheduleProviderWorkspaceRefresh(
          {
            providerId: providerRecordPanel.providerId,
            zoneId: providerRecordPanel.zoneId,
            zoneName: providerRecordPanel.zoneName,
            preserveDesiredInput: false,
          },
          { onErrorMessage: "记录已保存，但重新拉取 DNS 记录失败。" },
        );
      }
    },
    onError: (error) => {
      setChangeSetError(
        describeAdminProviderWorkspaceError(
          getAPIErrorMessage(error, "保存 DNS 记录到服务商失败"),
        ),
      );
    },
  });

  const validatingProviderID = validateProviderMutation.isPending
    ? validateProviderMutation.variables ?? null
    : null;
  const loadingZonesProviderID = loadProviderZonesMutation.isPending
    ? loadProviderZonesMutation.variables?.id ?? null
    : null;
  const deletingProviderID = deleteProviderMutation.isPending
    ? deleteProviderMutation.variables ?? null
    : null;
  const loadingRecordsZoneKey = loadProviderRecordsMutation.isPending
    ? `${loadProviderRecordsMutation.variables?.providerId ?? ""}:${loadProviderRecordsMutation.variables?.zoneId ?? ""}`
    : null;
  const isRefreshingDomainData = domainsQuery.isRefetching || providersQuery.isRefetching;

  const isChangeSetWorkspaceBusy =
    previewChangeSetMutation.isPending ||
    applyChangeSetMutation.isPending ||
    saveProviderRecordsMutation.isPending;

  const syncWorkspaceForRequestedDomain = useCallback(async (domain: DomainOption) => {
    setProviderZoneError(null);
    setChangeSetError(null);
    setChangeSetNotice(null);
    setChangeSetPreview(null);

    if (!domain.providerAccountId) {
      setProviderZoneError(`域名 ${domain.domain} 尚未绑定 DNS 服务商，请先回域名管理完成绑定。`);
      resetChangeWorkspace();
      return;
    }

    if (!(providersQuery.data ?? []).some((item) => item.id === domain.providerAccountId)) {
      setProviderZoneError("当前域名绑定的是独立私有 Provider，管理员 DNS 配置页不会接管这条绑定，请在对应面板处理或重新绑定管理员 Provider。");
      resetChangeWorkspace();
      return;
    }

    const providerId = domain.providerAccountId;
    const displayName = domain.providerDisplayName ?? domain.provider ?? "Provider";

    try {
      const zones =
        providerZonePanel?.providerId === providerId
          ? providerZonePanel.zones
          : await fetchAdminDomainProviderZones(providerId);

      setProviderZonePanel({
        providerId,
        displayName,
        zones,
      });
      setProviderZonesExpanded(true);
      setProviderZonesPage(1);

      const targetZone =
        zones.find((zone) => zone.name === domain.rootDomain) ??
        zones.find((zone) => zone.name === domain.domain) ??
        null;

      if (!targetZone) {
        setProviderZoneError(
          `已载入 ${displayName} 的 Zone 列表，但没有匹配到 ${domain.rootDomain} 或 ${domain.domain}。请先确认域名真实托管在当前 Provider 账号下。`,
        );
        resetChangeWorkspace({ keepZonePanel: true });
        return;
      }

      await refreshProviderRecordWorkspace({
        providerId,
        zoneId: targetZone.id,
        zoneName: targetZone.name,
      });
      setProviderRecordsExpanded(true);
      setVerificationExpanded(true);
      setChangeSetEditorExpanded(true);
      setChangeSetHistoryExpanded(true);
      setProviderRecordsPage(1);
      setChangeSetEditorPage(1);
      setChangeSetHistoryPage(1);
      setProviderActionNotice(`已按域名 ${domain.domain} 定位到 Zone ${targetZone.name}。`);
    } catch (error) {
      setProviderZoneError(
        describeAdminProviderWorkspaceError(
          getAPIErrorMessage(error, "暂时无法加载该域名的 DNS 工作区，请先校验连接或检查凭据。"),
        ),
      );
      resetChangeWorkspace({ keepZonePanel: true });
    }
  }, [providerZonePanel, providersQuery.data, refreshProviderRecordWorkspace, resetChangeWorkspace]);

  useEffect(() => {
    if (!requestedDomain && !requestedProvider) {
      autoWorkspaceRequestRef.current = null;
    }

    const invalidKeys: Array<"providerId" | "domainId"> = [];

    if (searchParams.has("domainId") && requestedDomainId !== null && !requestedDomain) {
      invalidKeys.push("domainId");
    }
    if (searchParams.has("providerId") && requestedProviderId !== null && !requestedProvider) {
      invalidKeys.push("providerId");
    }

    if (invalidKeys.length > 0) {
      autoWorkspaceRequestRef.current = null;
      clearInvalidSearchParams(invalidKeys);
      return;
    }

    if (requestedDomain) {
      const requestKey = `domain:${requestedDomain.id}:${requestedDomain.providerAccountId ?? 0}:${requestedDomain.rootDomain}:${requestedDomain.domain}`;

      if (!requestedDomain.providerAccountId) {
        autoWorkspaceRequestRef.current = requestKey;
        setProviderZoneError(`域名 ${requestedDomain.domain} 尚未绑定 DNS 服务商，请先回域名管理完成绑定。`);
        return;
      }

      if (
        !providerRecordPanel ||
        requestedDomain.providerAccountId !== providerRecordPanel.providerId ||
        (providerRecordPanel.zoneName !== requestedDomain.rootDomain &&
          providerRecordPanel.zoneName !== requestedDomain.domain)
      ) {
        if (
          !loadProviderZonesMutation.isPending &&
          autoWorkspaceRequestRef.current !== requestKey
        ) {
          autoWorkspaceRequestRef.current = requestKey;
          void syncWorkspaceForRequestedDomain(requestedDomain);
        }
        return;
      }

      autoWorkspaceRequestRef.current = requestKey;
    }

    if (!requestedProvider || providerZonePanel?.providerId === requestedProvider.id || loadProviderZonesMutation.isPending) {
      return;
    }

    const requestKey = `provider:${requestedProvider.id}`;
    if (autoWorkspaceRequestRef.current === requestKey) {
      return;
    }

    autoWorkspaceRequestRef.current = requestKey;
    loadProviderZonesMutation.mutate({
      id: requestedProvider.id,
      displayName: requestedProvider.displayName,
    });
  }, [
    clearInvalidSearchParams,
    loadProviderZonesMutation,
    location.pathname,
    providerRecordPanel,
    providerZonePanel?.providerId,
    requestedDomain,
    requestedDomainId,
    requestedProvider,
    requestedProviderId,
    searchParams,
    syncWorkspaceForRequestedDomain,
  ]);

  return (
    <WorkspacePage>
      <WorkspacePanel
        action={
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              onClick={() => {
                navigate("/admin/domains");
              }}
            >
              <Globe className="size-4" />
              域名管理
            </Button>
            <Button
              variant="outline"
              onClick={openCreateProviderDialog}
            >
              <Plus className="size-4" />
              新增 Provider 账号
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                void refreshAdminDomainData();
              }}
            >
              <RefreshCcw className={isRefreshingDomainData ? "size-4 animate-spin" : "size-4"} />
              刷新
            </Button>
          </div>
        }
        description="将 Zone、Records、验证与 Change Set 拆分为独立工作区。"
        title="DNS 配置"
      >
        <div className="space-y-4">
          <AlertDialog
            open={providerDeleteDialog !== null}
            onOpenChange={(open) => {
              if (!open) {
                setProviderDeleteDialog(null);
              }
            }}
          >
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>删除 DNS 服务商账号？</AlertDialogTitle>
                <AlertDialogDescription>
                  {providerDeleteDialog
                    ? `确认删除 Provider 账号 ${providerDeleteDialog.name}？删除后将无法继续读取 Zone，也无法继续修改对应 DNS 记录。`
                    : ""}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>取消</AlertDialogCancel>
                <AlertDialogAction
                  onClick={async () => {
                    if (!providerDeleteDialog) {
                      return;
                    }
                    const liveProvider = await resolveLiveAdminProvider(providerDeleteDialog.id);
                    if (!liveProvider) {
                      setProviderDeleteDialog(null);
                      return;
                    }
                    deleteProviderMutation.mutate(liveProvider.id);
                    setProviderDeleteDialog(null);
                  }}
                >
                  确认删除
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
          {(requestedProvider || requestedDomain || providerRecordPanel) ? (
            <Card className="border-border/60 bg-card/85 shadow-none">
              <CardContent className="flex flex-wrap items-center justify-between gap-3 py-4">
                <div className="space-y-1">
                  <div className="text-sm font-medium">当前上下文</div>
                  <p className="text-xs text-muted-foreground">
                    {requestedDomain
                      ? `已根据域名 ${requestedDomain.domain} 自动打开 DNS 工作区。`
                      : requestedProvider
                        ? `已根据 Provider ${requestedProvider.displayName} 自动定位 Zone 工作区。`
                        : `当前查看 ${providerRecordPanel?.zoneName ?? providerZonePanel?.displayName ?? "DNS 工作区"}。`}
                  </p>
                </div>
                <div className="flex flex-wrap gap-2">
                  {requestedDomain ? <WorkspaceBadge variant="outline">域名：{requestedDomain.domain}</WorkspaceBadge> : null}
                  {requestedProvider ? <WorkspaceBadge variant="outline">Provider：{requestedProvider.displayName}</WorkspaceBadge> : null}
                  {providerRecordPanel ? <WorkspaceBadge variant="outline">Zone：{providerRecordPanel.zoneName}</WorkspaceBadge> : null}
                  {requestedDomain ? <WorkspaceBadge variant="outline">根域：{requestedDomain.rootDomain}</WorkspaceBadge> : null}
                </div>
              </CardContent>
            </Card>
          ) : null}
          <Card className="border-border/60 bg-card/85 shadow-none">
            <CardContent className="space-y-3 py-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="space-y-1">
                  <div className="text-sm font-medium">操作提示</div>
                  <p className="text-xs text-muted-foreground">
                    先定位 Provider 或域名，再处理 Zone、验证和 Change Set；根域记录通常用 `@`，子域记录只填前缀即可。
                  </p>
                </div>
                <div className="flex flex-wrap gap-2">
                  <WorkspaceBadge variant="outline">1. 选择域名</WorkspaceBadge>
                  <WorkspaceBadge variant="outline">2. 校验记录</WorkspaceBadge>
                  <WorkspaceBadge variant="outline">3. 应用变更</WorkspaceBadge>
                </div>
              </div>
            </CardContent>
          </Card>
        <Dialog
          onOpenChange={(open) => {
            setCreateProviderDialogOpen(open);
            if (open) {
              setProviderMutationError(null);
            } else {
              resetProviderForm();
            }
          }}
          open={isCreateProviderDialogOpen}
        >
          <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-4xl">
            <DialogHeader>
              <DialogTitle>{isEditingProvider ? "编辑 Provider 账号" : "新增 Provider 账号"}</DialogTitle>
              <DialogDescription>
                {isEditingProvider
                  ? providerCoreFieldsLocked
                    ? "当前 Provider 已绑定域名，可继续更新显示名称、凭据、状态和权限，但不能改服务商类型或鉴权方式。"
                    : "当前 Provider 未绑定域名，服务商类型、鉴权方式、凭据与权限都可以直接修改。"
                  : "使用逐字段表单录入 DNS 服务商凭据，后续所有操作都通过可视化按钮完成。"}
              </DialogDescription>
            </DialogHeader>

            <div className="grid gap-5 lg:grid-cols-[1.1fr_0.9fr]">
              <div className="space-y-4">
                <div className="grid gap-4 md:grid-cols-2">
                  <WorkspaceField label="DNS 服务商">
                    <OptionCombobox
                      ariaLabel="DNS 服务商"
                      emptyLabel="没有匹配服务商"
                      onValueChange={(value) => {
                        const nextProvider = value || "cloudflare";
                        setProviderDraft((current) => ({
                          ...current,
                          provider: nextProvider,
                          authType: nextProvider === "spaceship" ? "api_key" : "api_token",
                          permissionValues: sanitizeProviderPermissions(
                            nextProvider,
                            current.permissionValues,
                          ),
                        }));
                        setProviderCredentials({
                          apiToken: "",
                          apiEmail: "",
                          apiKey: "",
                          apiSecret: "",
                        });
                      }}
                      options={[
                        { value: "cloudflare", label: "Cloudflare" },
                        { value: "spaceship", label: "Spaceship" },
                      ]}
                      placeholder="选择服务商"
                      searchPlaceholder="搜索服务商"
                      disabled={providerCoreFieldsLocked}
                      value={providerDraft.provider}
                    />
                  </WorkspaceField>

                  <WorkspaceField label="Owner">
                    <OptionCombobox
                      ariaLabel="Owner Type"
                      emptyLabel="没有匹配 Owner"
                      onValueChange={() => {}}
                      options={[
                        { value: "platform", label: "platform" },
                      ]}
                      placeholder="选择归属"
                      searchPlaceholder="搜索归属"
                      disabled
                      value={providerDraft.ownerType}
                    />
                  </WorkspaceField>

                  <WorkspaceField label="显示名称">
                    <Input
                      className="h-10"
                      onChange={(event) =>
                        setProviderDraft((current) => ({
                          ...current,
                          displayName: event.target.value,
                        }))
                      }
                      placeholder="例如：Cloudflare 主账号"
                      value={providerDraft.displayName}
                    />
                  </WorkspaceField>

                  <WorkspaceField label="状态">
                    <OptionCombobox
                      ariaLabel="Provider Status"
                      emptyLabel="没有匹配状态"
                      onValueChange={(value) =>
                        setProviderDraft((current) => ({
                          ...current,
                          status: value || "healthy",
                        }))
                      }
                      options={[
                        { value: "healthy", label: "healthy" },
                        { value: "degraded", label: "degraded" },
                        { value: "pending", label: "pending" },
                      ]}
                      placeholder="选择状态"
                      searchPlaceholder="搜索状态"
                      value={providerDraft.status}
                    />
                  </WorkspaceField>

                  <WorkspaceField label="鉴权方式">
                    <OptionCombobox
                      ariaLabel="Provider Auth Type"
                      emptyLabel="没有匹配鉴权方式"
                      onValueChange={(value) => {
                        setProviderDraft((current) => ({
                          ...current,
                          authType:
                            value ||
                            (current.provider === "spaceship" ? "api_key" : "api_token"),
                        }));
                        setProviderCredentials({
                          apiToken: "",
                          apiEmail: "",
                          apiKey: "",
                          apiSecret: "",
                        });
                      }}
                      options={
                        providerDraft.provider === "spaceship"
                          ? [{ value: "api_key", label: "API Key + API Secret" }]
                          : [
                              { value: "api_token", label: "API Token" },
                              { value: "api_key", label: "Global API Key + Email" },
                            ]
                      }
                      placeholder="选择鉴权方式"
                      searchPlaceholder="搜索鉴权方式"
                      disabled={providerCoreFieldsLocked}
                      value={providerDraft.authType}
                    />
                  </WorkspaceField>
                </div>

                <div className="rounded-2xl border border-border/60 bg-muted/20 p-4">
                  <div className="mb-3 space-y-1">
                    <p className="text-sm font-medium">凭据字段</p>
                    <p className="text-sm text-muted-foreground">
                      按服务商填写必要字段，不需要再手写 JSON 或 Secret Ref。
                    </p>
                  </div>

                  <div className="mb-4 rounded-xl border border-border/60 bg-background/70 px-4 py-3">
                    <p className="text-sm font-medium">
                      {getProviderAuthModeMeta(providerDraft.provider, providerDraft.authType).title}
                    </p>
                    <p className="mt-1 text-sm text-muted-foreground">
                      {getProviderAuthModeMeta(providerDraft.provider, providerDraft.authType).description}
                      {isEditingProvider ? " 留空则沿用当前已保存的凭据。" : ""}
                    </p>
                  </div>

                  <div className="grid gap-4">
                    {getProviderCredentialFields(providerDraft.provider, providerDraft.authType).map((field) => (
                      <WorkspaceField key={field.key} label={field.label}>
                        <Input
                          aria-label={field.label}
                          className="h-10"
                          onChange={(event) =>
                            setProviderCredentials((current) => ({
                              ...current,
                              [field.key]: event.target.value,
                            }))
                          }
                          placeholder={field.placeholder}
                          type={field.type}
                          value={providerCredentials[field.key]}
                        />
                      </WorkspaceField>
                    ))}
                  </div>
                </div>
              </div>

              <div className="space-y-4">
                <div className="rounded-2xl border border-border/60 bg-card p-4">
                  <div className="mb-3 space-y-1">
                    <p className="text-sm font-medium">权限</p>
                    <p className="text-sm text-muted-foreground">
                      选择这个 Provider 账号已授予的权限，后续操作会按权限范围展示。
                    </p>
                  </div>

                  <MultiOptionCombobox
                    ariaLabel="权限"
                    emptyLabel="没有可选权限"
                    options={getProviderPermissionOptions(providerDraft.provider)}
                    placeholder="选择需要的权限"
                    searchPlaceholder="继续搜索权限"
                    values={providerDraft.permissionValues}
                    onValuesChange={(values) =>
                      setProviderDraft((current) => ({
                        ...current,
                        permissionValues: sanitizeProviderPermissions(current.provider, values),
                      }))
                    }
                  />
                </div>

                <div className="rounded-2xl border border-border/60 bg-muted/20 p-4 text-sm text-muted-foreground">
                  <p className="font-medium text-foreground">提交后效果</p>
                  <p className="mt-2 leading-7">
                    平台会直接按这些字段保存凭据，并用于校验连接、读取 Zone、读取
                    Records 与应用变更。
                  </p>
                </div>
              </div>
            </div>

            <DialogFooter>
              {providerMutationError ? (
                <NoticeBanner autoHideMs={5000} className="mr-auto" onDismiss={() => setProviderMutationError(null)} variant="error">
                  {providerMutationError}
                </NoticeBanner>
              ) : null}
              <DialogClose asChild>
                <Button variant="outline">取消</Button>
              </DialogClose>
              <Button
                disabled={
                  createProviderMutation.isPending ||
                  updateProviderMutation.isPending ||
                  providerDraft.displayName.trim() === "" ||
                  !canSubmitProviderCredentials(
                    providerDraft.provider,
                    providerDraft.authType,
                    providerCredentials,
                    isEditingProvider,
                  )
                }
                onClick={() => {
                  const input = {
                    provider: providerDraft.provider,
                    ownerType: "platform",
                    displayName:
                      providerDraft.displayName ||
                      `${providerDraft.provider} account`,
                    authType: providerDraft.authType,
                    credentials: {
                      apiToken: providerCredentials.apiToken.trim(),
                      apiEmail: providerCredentials.apiEmail.trim(),
                      apiKey: providerCredentials.apiKey.trim(),
                      apiSecret: providerCredentials.apiSecret.trim(),
                    },
                    status: providerDraft.status,
                    capabilities: sanitizeProviderPermissions(
                      providerDraft.provider,
                      providerDraft.permissionValues,
                    ),
                  };

                  if (editingProviderId !== null) {
                    updateProviderMutation.mutate({
                      providerAccountId: editingProviderId,
                      input,
                    });
                    return;
                  }

                  createProviderMutation.mutate(input);
                }}
              >
                {createProviderMutation.isPending || updateProviderMutation.isPending
                  ? "提交中..."
                  : isEditingProvider
                    ? "保存 Provider 账号"
                    : "添加 Provider 账号"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog
          onOpenChange={(open) => {
            setCreateDomainDialogOpen(open);
            if (open) {
              setDomainMutationError(null);
            } else {
              setEditingDomainId(null);
              setDraft(emptyDomainDraft);
            }
          }}
          open={isCreateDomainDialogOpen}
        >
          <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-3xl">
            <DialogHeader>
              <DialogTitle>{isEditingDomain ? "编辑域名" : "添加域名"}</DialogTitle>
              <DialogDescription>
                {isEditingDomain
                  ? "直接调整域名状态、发布策略和 Provider 绑定；解除绑定后即可删除不再使用的 Provider。"
                  : "添加自定义域名后，需要配置 DNS 记录并完成验证。"}
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4">
              <WorkspaceField label="名称">
                <Input
                  className="h-12 rounded-xl text-base"
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      domain: event.target.value,
                    }))
                  }
                  placeholder="example.com"
                  value={draft.domain}
                />
              </WorkspaceField>

              <div className="grid gap-4 md:grid-cols-2">
                <WorkspaceField label="状态">
                  <OptionCombobox
                    ariaLabel="域名状态"
                    emptyLabel="没有匹配的状态"
                    onValueChange={(value) =>
                      setDraft((current) => ({ ...current, status: value }))
                    }
                    options={statusOptions}
                    placeholder="选择状态"
                    searchPlaceholder="搜索状态"
                    value={draft.status}
                  />
                </WorkspaceField>

                <WorkspaceField label="可见性">
                  <OptionCombobox
                    ariaLabel="域名可见性"
                    emptyLabel="没有匹配的可见性"
                    onValueChange={(value) =>
                      setDraft((current) => ({
                        ...current,
                        visibility: value || "private",
                      }))
                    }
                    options={visibilityOptions}
                    placeholder="选择可见性"
                    searchPlaceholder="搜索可见性"
                    value={draft.visibility}
                  />
                </WorkspaceField>
              </div>

              <div className="grid gap-4 md:grid-cols-2">
                <WorkspaceField label="发布状态">
                  <OptionCombobox
                    ariaLabel="域名发布状态"
                    emptyLabel="没有匹配的发布状态"
                    onValueChange={(value) =>
                      setDraft((current) => ({
                        ...current,
                        publicationStatus: value || "draft",
                      }))
                    }
                    options={publicationOptions}
                    placeholder="选择发布状态"
                    searchPlaceholder="搜索发布状态"
                    value={draft.publicationStatus}
                  />
                </WorkspaceField>

                <WorkspaceField label="DNS 服务商账号">
                  <div className="space-y-2">
                    <OptionCombobox
                      ariaLabel="DNS 服务商账号"
                      emptyLabel="没有匹配 Provider 账号"
                      onValueChange={(value) =>
                        setDraft((current) => ({
                          ...current,
                          providerAccountId: value || "",
                        }))
                      }
                      options={providerOptions}
                      placeholder="选择服务商账号"
                      searchPlaceholder="搜索服务商账号"
                      value={draft.providerAccountId || undefined}
                    />
                    {draft.providerAccountId ? (
                      <Button
                        className="h-9 px-3"
                        size="sm"
                        type="button"
                        variant="outline"
                        onClick={() =>
                          setDraft((current) => ({
                            ...current,
                            providerAccountId: "",
                          }))
                        }
                      >
                        解除 Provider 绑定
                      </Button>
                    ) : null}
                  </div>
                </WorkspaceField>
              </div>

              <div className="grid gap-4 md:grid-cols-2">
                <WorkspaceField label="健康状态">
                  <OptionCombobox
                    ariaLabel="域名健康状态"
                    emptyLabel="没有匹配的健康状态"
                    onValueChange={(value) =>
                      setDraft((current) => ({
                        ...current,
                        healthStatus: value || "unknown",
                      }))
                    }
                    options={[
                      { value: "healthy", label: "healthy" },
                      { value: "unknown", label: "unknown" },
                      { value: "degraded", label: "degraded" },
                    ]}
                    placeholder="选择健康状态"
                    searchPlaceholder="搜索健康状态"
                    value={draft.healthStatus}
                  />
                </WorkspaceField>

                <WorkspaceField label="权重">
                  <Input
                    className="h-9"
                    min={0}
                    onChange={(event) =>
                      setDraft((current) => ({
                        ...current,
                        weight: Number(event.target.value),
                      }))
                    }
                    type="number"
                    value={draft.weight}
                  />
                </WorkspaceField>
              </div>

              <div className="flex items-center gap-2">
                <Checkbox
                  checked={draft.isDefault}
                  id="admin-domain-default"
                  onCheckedChange={(checked) =>
                    setDraft((current) => ({
                      ...current,
                      isDefault: checked === true,
                    }))
                  }
                />
                <Label className="text-sm" htmlFor="admin-domain-default">
                  设为默认
                </Label>
              </div>
            </div>

            <DialogFooter>
              {domainMutationError ? (
                <NoticeBanner autoHideMs={5000} className="mr-auto" onDismiss={() => setDomainMutationError(null)} variant="error">
                  {domainMutationError}
                </NoticeBanner>
              ) : null}
              <DialogClose asChild>
                <Button variant="outline">取消</Button>
              </DialogClose>
              <Button
                disabled={upsertMutation.isPending || draft.domain.trim() === ""}
                onClick={() =>
                  upsertMutation.mutate({
                    ...draft,
                    providerAccountId: draft.providerAccountId
                      ? Number(draft.providerAccountId)
                      : undefined,
                    verificationScore:
                      draft.healthStatus === "healthy" ? 100 : 0,
                  })
                }
              >
                {upsertMutation.isPending ? "提交中..." : isEditingDomain ? "保存变更" : "添加"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog
          onOpenChange={(open) => {
            setGenerateSubdomainDialogOpen(open);
            if (open) {
              setSubdomainMutationError(null);
            }
          }}
          open={isGenerateSubdomainDialogOpen}
        >
          <DialogContent className="sm:max-w-2xl">
            <DialogHeader>
              <DialogTitle>批量生成子域名</DialogTitle>
              <DialogDescription>
                从现有根域批量生成子域前缀，适合统一下发 MX、relay、edge 等记录入口。
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-4">
              <WorkspaceField label="选择根域名">
                <OptionCombobox
                  ariaLabel="选择根域名"
                  emptyLabel="没有匹配根域名"
                  onValueChange={(value) =>
                    setSelectedBaseDomainId(value ? Number(value) : "")
                  }
                  options={rootDomains.map((item) => ({
                    value: String(item.id),
                    label: item.domain,
                    keywords: [item.rootDomain],
                  }))}
                  placeholder="选择根域名"
                  searchPlaceholder="搜索根域名"
                  value={
                    selectedBaseDomainId === ""
                      ? undefined
                      : String(selectedBaseDomainId)
                  }
                />
              </WorkspaceField>

              <WorkspaceField label="多级前缀">
                <Textarea
                  onChange={(event) => setPrefixInput(event.target.value)}
                  placeholder={"一行一个前缀，例如：\nmx\nmx.edge\nrelay.cn.hk"}
                  rows={6}
                  value={prefixInput}
                />
              </WorkspaceField>
            </div>

            <DialogFooter>
              {subdomainMutationError ? (
                <NoticeBanner autoHideMs={5000} className="mr-auto" onDismiss={() => setSubdomainMutationError(null)} variant="error">
                  {subdomainMutationError}
                </NoticeBanner>
              ) : null}
              <DialogClose asChild>
                <Button variant="outline">取消</Button>
              </DialogClose>
              <Button
                disabled={selectedBaseDomainId === "" || generateMutation.isPending}
                onClick={() =>
                  generateMutation.mutate({
                    baseDomainId: Number(selectedBaseDomainId),
                    prefixes: prefixInput
                      .split(/\r?\n/)
                      .map((item) => item.trim())
                      .filter(Boolean),
                    status: "active",
                    visibility: "private",
                    publicationStatus: "draft",
                    healthStatus: "unknown",
                    weight: 90,
                  })
                }
              >
                {generateMutation.isPending ? "提交中..." : "批量生成子域名"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <div className="grid gap-4">
          <Card className="border-border/60 bg-muted/10 shadow-none">
            <CardContent className="space-y-4 py-4">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="space-y-1">
                  <div className="text-sm font-medium">DNS 工作台</div>
                  <p className="text-sm text-muted-foreground">使用 Tabs 划分 Provider、Zone 与 Zone 工作区。</p>
                </div>
                <div className="flex flex-wrap gap-2">
                  <WorkspaceBadge variant="outline">
                    Provider {(providersQuery.data?.length ?? 0)}
                  </WorkspaceBadge>
                  <WorkspaceBadge variant="outline">
                    Zone {providerZonePanel?.zones.length ?? 0}
                  </WorkspaceBadge>
                  <WorkspaceBadge variant="outline">
                    Records {providerRecordPanel?.records.length ?? 0}
                  </WorkspaceBadge>
                </div>
              </div>

              {providerDeleteError ? (
                <NoticeBanner autoHideMs={5000} onDismiss={() => setProviderDeleteError(null)} variant="error">
                  {providerDeleteError}
                </NoticeBanner>
              ) : null}
              {providerActionNotice ? (
                <NoticeBanner autoHideMs={5000} onDismiss={() => setProviderActionNotice(null)} variant="success">
                  {providerActionNotice}
                </NoticeBanner>
              ) : null}
              {providerValidationError ? (
                <NoticeBanner autoHideMs={5000} onDismiss={() => setProviderValidationError(null)} variant="error">
                  {providerValidationError}
                </NoticeBanner>
              ) : null}
              {providerZoneError ? (
                <NoticeBanner autoHideMs={5000} onDismiss={() => setProviderZoneError(null)} variant="error">
                  {providerZoneError}
                </NoticeBanner>
              ) : null}

              <Tabs
                className="min-w-0"
                value={activeDnsTab}
                onValueChange={(value) => setActiveDnsTab(value as DnsWorkspaceTab)}
              >
                <TabsList className="w-full justify-start overflow-x-auto" variant="line">
                  <TabsTrigger value="providers">Provider 账号</TabsTrigger>
                  <TabsTrigger value="zones">Zone</TabsTrigger>
                  <TabsTrigger value="records">Zone 工作区</TabsTrigger>
                </TabsList>

                <TabsContent className="space-y-3" value="providers">
              {providersQuery.data?.length ? (
                <div className="space-y-3">
                  {paginatedProviders.items.map((provider) => (
                    <WorkspaceListRow
                      key={provider.id}
                      title={provider.displayName}
                      description={`${provider.provider} · ${provider.ownerType} · ${provider.authType}`}
                      meta={
                        <>
                          <WorkspaceBadge>{provider.status}</WorkspaceBadge>
                          <span>
                            {provider.hasSecret
                              ? "secret ready"
                              : "secret missing"}
                          </span>
                          <span>{provider.capabilities.join(", ")}</span>
                          <Button
                            aria-label={`${provider.displayName} 校验连接`}
                            disabled={
                              validatingProviderID === provider.id ||
                              loadingZonesProviderID === provider.id ||
                              deletingProviderID === provider.id
                            }
                            size="sm"
                            variant="outline"
                            onClick={async () => {
                              setProviderActionNotice(null);
                              setProviderValidationError(null);
                              const liveProvider = await resolveLiveAdminProvider(provider.id);
                              if (!liveProvider) {
                                return;
                              }
                              validateProviderMutation.mutate(liveProvider.id);
                            }}
                          >
                            {validatingProviderID === provider.id ? "校验中..." : "校验连接"}
                          </Button>
                          <Button
                            aria-label={`${provider.displayName} 编辑`}
                            disabled={
                              deletingProviderID === provider.id ||
                              validatingProviderID === provider.id ||
                              loadingZonesProviderID === provider.id
                            }
                            size="sm"
                            variant="ghost"
                            onClick={() => openEditProviderDialog(provider)}
                          >
                            编辑
                          </Button>
                          <Button
                            aria-label={`${provider.displayName} 查看 Zones`}
                            disabled={
                              validatingProviderID === provider.id ||
                              loadingZonesProviderID === provider.id ||
                              deletingProviderID === provider.id
                            }
                            size="sm"
                            variant="ghost"
                            onClick={async () => {
                              setProviderActionNotice(null);
                              setProviderZoneError(null);
                              const liveProvider = await resolveLiveAdminProvider(provider.id);
                              if (!liveProvider) {
                                return;
                              }
                              loadProviderZonesMutation.mutate({
                                id: liveProvider.id,
                                displayName: liveProvider.displayName,
                              });
                            }}
                          >
                            {loadingZonesProviderID === provider.id ? "载入中..." : "查看 Zones"}
                          </Button>
                          <Button
                            aria-label={`${provider.displayName} 删除`}
                            disabled={
                              deletingProviderID === provider.id ||
                              validatingProviderID === provider.id ||
                              loadingZonesProviderID === provider.id
                            }
                            size="sm"
                            variant="ghost"
                            onClick={() => {
                              setProviderActionNotice(null);
                              setProviderDeleteDialog({
                                id: provider.id,
                                name: provider.displayName,
                              });
                            }}
                          >
                            {deletingProviderID === provider.id ? "删除中..." : "删除"}
                          </Button>
                        </>
                      }
                    />
                  ))}
                  <PaginationControls
                    itemLabel="Provider 账号"
                    page={paginatedProviders.page}
                    pageSize={ADMIN_PROVIDERS_PAGE_SIZE}
                    total={paginatedProviders.total}
                    totalPages={paginatedProviders.totalPages}
                    onPageChange={setProviderAccountsPage}
                  />
                </div>
              ) : (
                <WorkspaceEmpty
                  title="暂无 Provider 账号"
                  description="先新增 DNS 服务商账号，再继续查看 Zone 和 Zone 工作区。"
                />
              )}
                </TabsContent>

                <TabsContent className="space-y-3" value="zones">

              {providerZonePanel ? (
                <div className="space-y-3 rounded-xl border border-border/60 bg-background/60 p-3">
                  <SectionToggle
                    description={`共 ${providerZonePanel.zones.length} 个可用 Zone`}
                    expanded={providerZonesExpanded}
                    meta={
                      <WorkspaceBadge variant="outline">
                        {providerZonePanel.displayName}
                      </WorkspaceBadge>
                    }
                    title={`${providerZonePanel.displayName} · Zones`}
                    onToggle={() => setProviderZonesExpanded((current) => !current)}
                  />

                  {providerZonesExpanded && providerZonePanel.zones.length ? (
                    <div className="space-y-2">
                      {paginatedZones.items.map((zone) => (
                        (() => {
                          const zoneKey = `${providerZonePanel.providerId}:${zone.id}`;
                          const cooldownUntil = zoneFailureCooldowns[zoneKey] ?? 0;
                          const cooldownSeconds = cooldownUntil > Date.now()
                            ? Math.max(1, Math.ceil((cooldownUntil - Date.now()) / 1000))
                            : 0;
                          return (
                        <WorkspaceListRow
                          className={cn(
                            providerRecordPanel?.zoneId === zone.id
                              ? "border-primary/45 bg-primary/5"
                              : undefined,
                          )}
                          key={zone.id}
                          title={zone.name}
                          description={
                            providerRecordPanel?.zoneId === zone.id
                              ? "Provider Zone · 当前工作区"
                              : "Provider Zone"
                          }
                          meta={
                            <>
                              <WorkspaceBadge>{zone.status}</WorkspaceBadge>
                              {providerRecordPanel?.zoneId === zone.id ? (
                                <WorkspaceBadge variant="outline">当前工作区</WorkspaceBadge>
                              ) : null}
                              {cooldownSeconds > 0 ? (
                                <WorkspaceBadge variant="outline">冷却 {cooldownSeconds}s</WorkspaceBadge>
                              ) : null}
                              <Button
                                aria-label={`${zone.name} 查看 Records`}
                                disabled={
                                  loadingRecordsZoneKey === zoneKey ||
                                  previewChangeSetMutation.isPending ||
                                  applyChangeSetMutation.isPending ||
                                  saveProviderRecordsMutation.isPending ||
                                  cooldownSeconds > 0
                                }
                                size="sm"
                                variant="ghost"
                                onClick={() => {
                                  setChangeSetNotice(null);
                                  loadProviderRecordsMutation.mutate({
                                    providerId: providerZonePanel.providerId,
                                    zoneId: zone.id,
                                    zoneName: zone.name,
                                  });
                                }}
                              >
                                {loadingRecordsZoneKey === zoneKey
                                  ? "载入中..."
                                  : cooldownSeconds > 0
                                    ? `冷却 ${cooldownSeconds}s`
                                    : "查看 Records"}
                              </Button>
                            </>
                          }
                        />
                          );
                        })()
                      ))}
                      <PaginationControls
                        itemLabel="Zone"
                        page={paginatedZones.page}
                        pageSize={ADMIN_ZONES_PAGE_SIZE}
                        total={paginatedZones.total}
                        totalPages={paginatedZones.totalPages}
                        onPageChange={setProviderZonesPage}
                      />
                    </div>
                  ) : providerZonesExpanded ? (
                    <WorkspaceEmpty
                      title="暂无 Zone"
                      description="当前 Provider 账号还没有可用的 Zone。"
                    />
                  ) : null}
                </div>
              ) : (
                <WorkspaceEmpty
                  title="先查看 Zone"
                  description="在 Provider 账号页点击“查看 Zones”后，这里会显示当前 Provider 的 Zone 列表。"
                />
              )}
                </TabsContent>

                <TabsContent className="space-y-3" value="records">

              {providerRecordPanel ? (
                <div className="space-y-3 rounded-xl border border-border/60 bg-background/60 p-3">
                  <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border/60 bg-background/80 px-3 py-3">
                    <div>
                      <div className="text-sm font-medium">{providerRecordPanel.zoneName} · DNS Workspace</div>
                      <p className="text-xs text-muted-foreground">Records、Verification Health 和 DNS Change Set 现在为同级并排面板。</p>
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <WorkspaceBadge variant="outline">
                        Zone ID · {providerRecordPanel.zoneId}
                      </WorkspaceBadge>
                      <WorkspaceBadge variant="outline">
                        历史 {sortedChangeSetHistory.length}
                      </WorkspaceBadge>
                      <WorkspaceBadge variant="outline">
                        已验证 {verificationStatusSummary.verified}
                      </WorkspaceBadge>
                      <WorkspaceBadge variant="outline">
                        待修复 {verificationStatusSummary.drifted}
                      </WorkspaceBadge>
                      <Button
                        disabled={
                          isChangeSetWorkspaceBusy ||
                          loadingRecordsZoneKey === `${providerRecordPanel.providerId}:${providerRecordPanel.zoneId}`
                        }
                        size="sm"
                        type="button"
                        variant="ghost"
                        onClick={() => {
                          setProviderZoneError(null);
                          void refreshProviderRecordWorkspace({
                            providerId: providerRecordPanel.providerId,
                            zoneId: providerRecordPanel.zoneId,
                            zoneName: providerRecordPanel.zoneName,
                            preserveDesiredInput: true,
                            force: true,
                          })
                            .then(() => {
                              setChangeSetNotice(`已刷新 ${providerRecordPanel.zoneName} 的 DNS 工作区。`);
                            })
                            .catch((error) => {
                              setProviderZoneError(
                                describeAdminProviderWorkspaceError(
                                  getAPIErrorMessage(error, "刷新 DNS 工作区失败"),
                                ),
                              );
                            });
                        }}
                      >
                        <RefreshCcw className={loadingRecordsZoneKey === `${providerRecordPanel.providerId}:${providerRecordPanel.zoneId}` ? "size-4 animate-spin" : "size-4"} />
                        刷新当前 Zone
                      </Button>
                    </div>
                  </div>

                  <div className="space-y-3">
                    <div className="space-y-3 rounded-xl border border-border/60 bg-background/80 p-3">
                      <SectionToggle
                        description={`共 ${providerRecordPanel.records.length} 条 DNS Records`}
                        expanded={providerRecordsExpanded}
                        meta={
                          <WorkspaceBadge variant="outline">
                            第 {paginatedRecords.page} / {paginatedRecords.totalPages} 页
                          </WorkspaceBadge>
                        }
                        title="DNS Records"
                        onToggle={() => setProviderRecordsExpanded((current) => !current)}
                      />

                      {providerRecordsExpanded && providerRecordPanel.records.length ? (
                        <div className="space-y-2">
                          {paginatedRecords.items.map((record) => (
                            <WorkspaceListRow
                              key={record.id}
                              title={`${record.type} · ${record.name}`}
                              description={formatDNSRecordValueForDisplay(
                                record.type,
                                record.value,
                                currentRecordProviderType,
                              )}
                              descriptionClassName="font-mono text-xs break-all whitespace-normal"
                              meta={
                                <>
                                  <WorkspaceBadge>TTL {record.ttl}</WorkspaceBadge>
                                  {record.priority > 0 ? (
                                    <span>prio {record.priority}</span>
                                  ) : null}
                                  <span>
                                    {record.proxied ? "proxied" : "dns only"}
                                  </span>
                                </>
                              }
                            />
                          ))}
                          <PaginationControls
                            itemLabel="Record"
                            page={paginatedRecords.page}
                            pageSize={ADMIN_RECORDS_PAGE_SIZE}
                            total={paginatedRecords.total}
                            totalPages={paginatedRecords.totalPages}
                            onPageChange={setProviderRecordsPage}
                          />
                        </div>
                      ) : providerRecordsExpanded ? (
                        <WorkspaceEmpty
                          title="暂无 Records"
                          description="当前 Zone 还没有可读取的 DNS Records。"
                        />
                      ) : null}
                    </div>

                    <div className="space-y-3 rounded-xl border border-border/60 bg-background/80 p-3">
                      <SectionToggle
                        description="检查当前 Zone 的所有权、收信与发信记录是否符合平台建议。"
                        expanded={verificationExpanded}
                        meta={<WorkspaceBadge variant="outline">{verificationProfiles.length} 项</WorkspaceBadge>}
                        title="Verification Health"
                        onToggle={() => setVerificationExpanded((current) => !current)}
                      />

                      {verificationExpanded ? (
                        verificationProfiles.length ? (
                          <div className="space-y-2">
                            {verificationProfiles.map((profile) => (
                              <WorkspaceListRow
                                key={profile.verificationType}
                                title={profile.summary}
                                description={`${profile.verificationType} · observed ${profile.observedRecords.length} / expected ${profile.expectedRecords.length}`}
                                meta={
                                  <>
                                    <WorkspaceBadge>{profile.status}</WorkspaceBadge>
                                    <span>
                                      {profile.lastCheckedAt
                                        ? `检查于 ${formatChangeSetTimestamp(profile.lastCheckedAt)}`
                                        : "未记录检查时间"}
                                    </span>
                                    <Button
                                      disabled={!profile.repairRecords.length}
                                      size="sm"
                                      variant="outline"
                                      onClick={() => {
                                        setChangeSetNotice(`已载入 ${profile.verificationType} 修复建议。`);
                                        setDesiredRecordsDraft((current) =>
                                          applyVerificationRepairRecords(
                                            current,
                                            profile.repairRecords,
                                          ),
                                        );
                                      }}
                                    >
                                      加载 {profile.verificationType} 建议
                                    </Button>
                                  </>
                                }
                              />
                            ))}
                          </div>
                        ) : (
                          <WorkspaceEmpty
                            title="暂无验证结果"
                            description="当前 Zone 还没有生成可展示的 Verification Health 数据。"
                          />
                        )
                      ) : null}
                    </div>

                    <div className="space-y-3 rounded-xl border border-border/60 bg-muted/20 p-3">
                    <SectionToggle
                      description="逐行编辑目标记录，先生成 preview，再按 change-set 应用。"
                      expanded={changeSetEditorExpanded}
                      meta={<WorkspaceBadge variant="outline">{desiredRecordsDraft.length} 条目标记录</WorkspaceBadge>}
                      title="DNS Change Set"
                      onToggle={() => setChangeSetEditorExpanded((current) => !current)}
                    />

                    {changeSetEditorExpanded ? (
                    <>
                    <div className="space-y-3">
                      <div className="grid gap-2">
                        {paginatedChangeSetEditorRecords.items.map((record, index) => (
                          <div
                            key={record.localId}
                            className="rounded-xl border border-border/60 bg-background/80 p-3"
                          >
                            <div className="grid gap-3 xl:grid-cols-[110px_1.2fr_1.8fr_90px_90px_auto_auto]">
                              <DNSRecordTypeCombobox
                                disabled={isChangeSetWorkspaceBusy}
                                value={record.type}
                                onValueChange={(nextValue) =>
                                  setDesiredRecordsDraft((current) =>
                                    current.map((item) =>
                                      item.localId === record.localId
                                        ? {
                                            ...item,
                                            type: nextValue,
                                          }
                                        : item,
                                    ),
                                  )
                                }
                              />
                              <Input
                                aria-label="记录名称"
                                className="h-9"
                                disabled={isChangeSetWorkspaceBusy}
                                onChange={(event) =>
                                  setDesiredRecordsDraft((current) =>
                                    current.map((item) =>
                                      item.localId === record.localId
                                        ? {
                                            ...item,
                                            name: event.target.value,
                                          }
                                        : item,
                                    ),
                                  )
                                }
                                placeholder="@ / _dmarc"
                                value={record.name}
                              />
                              <Input
                                aria-label="记录值"
                                className="h-9"
                                disabled={isChangeSetWorkspaceBusy}
                                onChange={(event) =>
                                  setDesiredRecordsDraft((current) =>
                                    current.map((item) =>
                                      item.localId === record.localId
                                        ? {
                                            ...item,
                                            value: event.target.value,
                                          }
                                        : item,
                                    ),
                                  )
                                }
                                placeholder="记录值"
                                value={record.value}
                              />
                              <Input
                                aria-label="TTL"
                                className="h-9"
                                disabled={isChangeSetWorkspaceBusy}
                                min={60}
                                onChange={(event) =>
                                  setDesiredRecordsDraft((current) =>
                                    current.map((item) =>
                                      item.localId === record.localId
                                        ? {
                                            ...item,
                                            ttl: Number(event.target.value || 0),
                                          }
                                        : item,
                                    ),
                                  )
                                }
                                type="number"
                                value={record.ttl}
                              />
                              <Input
                                aria-label="优先级"
                                className="h-9"
                                disabled={isChangeSetWorkspaceBusy}
                                min={0}
                                onChange={(event) =>
                                  setDesiredRecordsDraft((current) =>
                                    current.map((item) =>
                                      item.localId === record.localId
                                        ? {
                                            ...item,
                                            priority: Number(event.target.value || 0),
                                          }
                                        : item,
                                    ),
                                  )
                                }
                                type="number"
                                value={record.priority}
                              />
                              <label className="flex items-center gap-2 text-sm text-muted-foreground">
                                <Checkbox
                                  aria-label="是否代理"
                                  checked={record.proxied}
                                  disabled={isChangeSetWorkspaceBusy}
                                  onCheckedChange={(checked) =>
                                    setDesiredRecordsDraft((current) =>
                                      current.map((item) =>
                                        item.localId === record.localId
                                          ? {
                                              ...item,
                                              proxied: checked === true,
                                            }
                                          : item,
                                      ),
                                    )
                                  }
                                />
                                代理
                              </label>
                              <Button
                                aria-label="删除记录"
                                className="h-9"
                                disabled={isChangeSetWorkspaceBusy}
                                size="sm"
                                variant="outline"
                                onClick={() =>
                                  setDesiredRecordsDraft((current) =>
                                    current.filter((item) => item.localId !== record.localId),
                                  )
                                }
                              >
                                删除
                              </Button>
                            </div>
                            <p className="mt-2 text-xs text-muted-foreground">
                              记录 #{(paginatedChangeSetEditorRecords.page - 1) * ADMIN_CHANGESET_EDITOR_PAGE_SIZE + index + 1}
                            </p>
                          </div>
                        ))}
                      </div>
                      <PaginationControls
                        itemLabel="目标记录"
                        page={paginatedChangeSetEditorRecords.page}
                        pageSize={ADMIN_CHANGESET_EDITOR_PAGE_SIZE}
                        total={paginatedChangeSetEditorRecords.total}
                        totalPages={paginatedChangeSetEditorRecords.totalPages}
                        onPageChange={setChangeSetEditorPage}
                      />

                      <div className="flex flex-wrap gap-2">
                      <Button
                        aria-label="新增记录"
                        disabled={isChangeSetWorkspaceBusy}
                        size="sm"
                        variant="outline"
                          onClick={() =>
                            setDesiredRecordsDraft((current) => {
                              const next = [
                                ...current,
                                createEditableProviderRecord({
                                  name: providerRecordPanel.zoneName,
                                }),
                              ];
                              setChangeSetEditorPage(
                                Math.max(1, Math.ceil(next.length / ADMIN_CHANGESET_EDITOR_PAGE_SIZE)),
                              );
                              return next;
                            })
                          }
                        >
                          <Plus className="size-4" />
                          新增记录
                        </Button>
                      <Button
                        disabled={isChangeSetWorkspaceBusy}
                        size="sm"
                        variant="ghost"
                          onClick={() => {
                            setDesiredRecordsDraft(
                              recordsToEditable(providerRecordPanel.records),
                            );
                            setChangeSetEditorPage(1);
                          }}
                        >
                          重置为当前记录
                        </Button>
                      </div>
                    </div>

                    {changeSetError ? (
                      <NoticeBanner autoHideMs={5000} className="text-xs" onDismiss={() => setChangeSetError(null)} variant="error">
                        {changeSetError}
                      </NoticeBanner>
                    ) : null}
                    {changeSetNotice ? (
                      <NoticeBanner autoHideMs={5000} className="text-xs" onDismiss={() => setChangeSetNotice(null)} variant="success">
                        {changeSetNotice}
                      </NoticeBanner>
                    ) : null}

                    <div className="flex flex-wrap gap-2">
                      <Button
                        disabled={isChangeSetWorkspaceBusy}
                        onClick={() => {
                          setChangeSetNotice(null);
                          saveProviderRecordsMutation.mutate({
                            providerId: providerRecordPanel.providerId,
                            zoneId: providerRecordPanel.zoneId,
                            zoneName: providerRecordPanel.zoneName,
                            records: editableToProviderRecords(desiredRecordsDraft),
                          });
                        }}
                      >
                        {saveProviderRecordsMutation.isPending ? "保存中..." : "保存到服务商"}
                      </Button>

                      <Button
                        disabled={isChangeSetWorkspaceBusy}
                        variant="outline"
                        onClick={() => {
                          setChangeSetNotice(null);
                          previewChangeSetMutation.mutate({
                            providerId: providerRecordPanel.providerId,
                            zoneId: providerRecordPanel.zoneId,
                            zoneName: providerRecordPanel.zoneName,
                            records: editableToProviderRecords(desiredRecordsDraft),
                          });
                        }}
                      >
                        {previewChangeSetMutation.isPending ? "生成中..." : "预览自动配置"}
                      </Button>

                      <Button
                        disabled={!changeSetPreview || isChangeSetWorkspaceBusy}
                        variant="secondary"
                        onClick={() => {
                          setChangeSetNotice(null);
                          if (changeSetPreview) {
                            applyChangeSetMutation.mutate(changeSetPreview.id);
                          }
                        }}
                      >
                        {applyChangeSetMutation.isPending ? "应用中..." : "应用自动配置"}
                      </Button>
                    </div>

                    {changeSetPreview ? (
                      <div className="space-y-2 rounded-xl border border-border/60 bg-background/80 p-3">
                        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                          <WorkspaceBadge variant="outline">当前预览</WorkspaceBadge>
                          <WorkspaceBadge>
                            {changeSetPreview.status}
                          </WorkspaceBadge>
                          <span>{describeChangeSetOperations(changeSetPreview)}</span>
                          <span>生成于 {formatChangeSetTimestamp(changeSetPreview.createdAt)}</span>
                          {changeSetPreview.appliedAt ? (
                            <span>应用于 {formatChangeSetTimestamp(changeSetPreview.appliedAt)}</span>
                          ) : (
                            <span>待应用</span>
                          )}
                        </div>

                        {changeSetPreview.operations.length ? (
                          <div className="space-y-2">
                            {changeSetPreview.operations.map((operation) => (
                              <WorkspaceListRow
                                key={operation.id}
                                title={`${operation.operation} · ${operation.recordType} · ${operation.recordName}`}
                                description={
                                  operation.after?.value ??
                                  operation.before?.value ??
                                  `${operation.recordType} ${operation.recordName}`
                                }
                                descriptionClassName="font-mono text-xs break-all whitespace-normal"
                                meta={
                                  <>
                                    <WorkspaceBadge>
                                      {operation.status}
                                    </WorkspaceBadge>
                                    {operation.after?.ttl ? (
                                      <span>TTL {operation.after.ttl}</span>
                                    ) : null}
                                    {operation.before?.ttl &&
                                    !operation.after?.ttl ? (
                                      <span>TTL {operation.before.ttl}</span>
                                    ) : null}
                                  </>
                                }
                              />
                            ))}
                          </div>
                        ) : (
                          <WorkspaceEmpty
                            title="无变更"
                            description="当前目标 Records 与上游记录已经一致。"
                          />
                        )}
                      </div>
                    ) : null}
                    </>
                    ) : null}

                    <div className="space-y-2 rounded-xl border border-border/60 bg-background/80 p-3">
                      <SectionToggle
                        description="当前 Zone 最近的 preview / apply 历史。"
                        expanded={changeSetHistoryExpanded}
                        meta={<WorkspaceBadge variant="outline">{sortedChangeSetHistory.length} 条历史</WorkspaceBadge>}
                        title="Change Set 历史"
                        onToggle={() => setChangeSetHistoryExpanded((current) => !current)}
                      />

                      {changeSetHistoryExpanded && sortedChangeSetHistory.length ? (
                        <div className="space-y-2">
                          {paginatedChangeSetHistory.items.map((item) => (
                            <WorkspaceListRow
                              className={cn(
                                selectedChangeSetID === item.id
                                  ? "border-emerald-500/40 bg-emerald-500/5"
                                  : undefined,
                              )}
                              key={item.id}
                              title={`#${item.id} · ${item.summary}`}
                              description={`${item.provider} · ${item.zoneName} · ${describeChangeSetOperations(item)}`}
                              meta={
                                <>
                                  <WorkspaceBadge>{item.status}</WorkspaceBadge>
                                  <span>{item.appliedAt ? "已应用" : "待应用"}</span>
                                  <span>
                                    {item.appliedAt
                                      ? `应用于 ${formatChangeSetTimestamp(item.appliedAt)}`
                                      : `生成于 ${formatChangeSetTimestamp(item.createdAt)}`}
                                  </span>
                                  {item.operations.length ? (
                                    <span>{item.operations.length} 条操作</span>
                                  ) : (
                                    <span>无操作</span>
                                  )}
                                  <Button
                                    disabled={isChangeSetWorkspaceBusy}
                                    size="sm"
                                    type="button"
                                    variant="outline"
                                    onClick={() => {
                                      setChangeSetPreview(item);
                                      setSelectedChangeSetID(item.id);
                                      setChangeSetError(null);
                                      setChangeSetNotice(`已载入历史 Change Set #${item.id}。`);
                                    }}
                                  >
                                    回看
                                  </Button>
                                  <Button
                                    disabled={
                                      isChangeSetWorkspaceBusy ||
                                      !item.operations.some((operation) => operation.after)
                                    }
                                    size="sm"
                                    type="button"
                                    variant="ghost"
                                  onClick={() => {
                                      const records = restoreEditableRecordsFromChangeSet(
                                        providerRecordPanel?.records ?? [],
                                        item,
                                      );
                                      setDesiredRecordsDraft(records);
                                      setChangeSetPreview(item);
                                      setSelectedChangeSetID(item.id);
                                      setChangeSetError(null);
                                      setChangeSetNotice(`已从 Change Set #${item.id} 恢复到编辑器。`);
                                    }}
                                  >
                                    恢复到编辑器
                                  </Button>
                                </>
                              }
                            />
                          ))}
                          <PaginationControls
                            itemLabel="Change Set"
                            page={paginatedChangeSetHistory.page}
                            pageSize={ADMIN_CHANGESETS_PAGE_SIZE}
                            total={paginatedChangeSetHistory.total}
                            totalPages={paginatedChangeSetHistory.totalPages}
                            onPageChange={setChangeSetHistoryPage}
                          />
                        </div>
                      ) : changeSetHistoryExpanded ? (
                        <WorkspaceEmpty
                          title="暂无 Change Set 历史"
                          description="当前 Zone 还没有 preview / apply 记录。"
                        />
                      ) : null}
                    </div>
                    </div>
                  </div>
                </div>
              ) : (
                <WorkspaceEmpty
                  title="先进入 Zone 工作区"
                  description="在 Zone 页点击“查看 Records”后，这里会显示 Records、验证和 Change Set。"
                />
              )}
                </TabsContent>
              </Tabs>
            </CardContent>
          </Card>

        </div>

        {/* Domain-level verification and DNS guidance moved to the domain management page. */}
        <NoticeBanner variant="info">
          待验证域名、验证入口和域名级引导已移动到 `域名管理` 页面；这里现在只处理 Provider 账号、Zone、Records、Verification 和 Change Set。
        </NoticeBanner>
        </div>
      </WorkspacePanel>
    </WorkspacePage>
  );
}


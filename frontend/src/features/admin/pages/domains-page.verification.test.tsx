import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fetchAdminDomainProviders, fetchAdminDomains, fetchAdminSettingsSections, verifyAdminDomain } from "../api";
import { AdminDomainsPage } from "./domains-page";

vi.mock("../api", () => ({
  fetchAdminDomains: vi.fn(),
  fetchAdminDomainProviders: vi.fn(),
  fetchAdminSettingsSections: vi.fn(),
  upsertAdminDomain: vi.fn(),
  deleteAdminDomain: vi.fn(),
  generateAdminSubdomains: vi.fn(),
  reviewAdminDomainPublication: vi.fn(),
  verifyAdminDomain: vi.fn(),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <AdminDomainsPage />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

describe("AdminDomainsPage verification", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    vi.mocked(fetchAdminSettingsSections).mockResolvedValue([
      {
        key: "mail",
        title: "Mail",
        description: "",
        items: [
          {
            key: "mail.smtp",
            value: {
              enabled: true,
              listenAddr: ":2525",
              hostname: "mail.mmjs.top",
              dkimCnameTarget: "shiro._domainkey.mmjs.top",
              maxMessageBytes: 10485760,
            },
            updatedBy: 1,
            updatedAt: "2026-04-03T10:00:00Z",
          },
        ],
      },
    ]);
    vi.mocked(fetchAdminDomainProviders).mockResolvedValue([]);
    vi.mocked(fetchAdminDomains).mockResolvedValue([
      {
        id: 7,
        domain: "provider-bound.test",
        status: "active",
        ownerUserId: 3,
        visibility: "private",
        publicationStatus: "draft",
        verificationScore: 0,
        healthStatus: "degraded",
        providerAccountId: 1,
        providerDisplayName: "Primary Cloudflare",
        isDefault: false,
        weight: 100,
        rootDomain: "provider-bound.test",
        parentDomain: "",
        level: 0,
        kind: "root",
      },
    ]);
    vi.mocked(verifyAdminDomain).mockResolvedValue({
      domain: {
        id: 7,
        domain: "provider-bound.test",
        status: "active",
        ownerUserId: 3,
        visibility: "private",
        publicationStatus: "draft",
        verificationScore: 50,
        healthStatus: "degraded",
        providerAccountId: 1,
        providerDisplayName: "Primary Cloudflare",
        isDefault: false,
        weight: 100,
        rootDomain: "provider-bound.test",
        parentDomain: "",
        level: 0,
        kind: "root",
      },
      passed: false,
      summary: "DNS 传播验证未通过，请根据缺失或漂移记录继续修复。",
      zoneName: "provider-bound.test",
      verifiedCount: 1,
      totalCount: 2,
      profiles: [
        {
          verificationType: "dmarc",
          status: "drifted",
          summary: "DMARC 策略仍未对齐",
          expectedRecords: [],
          observedRecords: [],
          repairRecords: [
            {
              type: "TXT",
              name: "_dmarc.provider-bound.test",
              value: "v=DMARC1; p=quarantine",
              ttl: 300,
              priority: 0,
              proxied: false,
            },
          ],
          lastCheckedAt: "2026-04-05T05:30:00Z",
        },
      ],
    });
  });

  it("shows verification details and dns link after verify", async () => {
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "验证" }));

    await waitFor(() => {
      expect(vi.mocked(verifyAdminDomain)).toHaveBeenCalled();
    });
    expect(vi.mocked(verifyAdminDomain).mock.calls[0]?.[0]).toBe(7);

    expect(await screen.findByText("最近一次验证未通过")).toBeInTheDocument();
    expect(screen.getByText("传播部分通过")).toBeInTheDocument();
    expect(screen.getByText("DMARC 策略仍未对齐")).toBeInTheDocument();
    expect(screen.getAllByText(/_dmarc\.provider-bound\.test/).length).toBeGreaterThan(0);
    expect(screen.getByRole("link", { name: "前往 DNS 配置" })).toHaveAttribute(
      "href",
      "/admin/dns?domainId=7&providerId=1",
    );
  });
});

import { describe, expect, it } from "vitest";
import {
  normalizeCommaSeparatedList,
  validateEmailAddress,
  validateHTTPUrl,
  validateIntegerRange,
  validateMailboxLocalPart,
  validateOneTimeCode,
  validateRequiredText,
  validateSelection,
} from "./validation";

describe("validation helpers", () => {
  it("validates required text boundaries", () => {
    expect(validateRequiredText("标题", "", { minLength: 2 })).toBe("标题不能为空。");
    expect(validateRequiredText("标题", "a", { minLength: 2 })).toBe("标题至少需要 2 个字符。");
    expect(validateRequiredText("标题", "abcd", { maxLength: 3 })).toBe("标题不能超过 3 个字符。");
    expect(validateRequiredText("标题", "正常标题", { minLength: 2, maxLength: 20 })).toBeNull();
  });

  it("validates email and webhook urls", () => {
    expect(validateEmailAddress("bad")).toBe("邮箱地址格式不正确。");
    expect(validateEmailAddress("user@example.com")).toBeNull();
    expect(validateHTTPUrl("ftp://example.com")).toBe("回调地址必须使用 http:// 或 https://。");
    expect(validateHTTPUrl("https://example.com/hook")).toBeNull();
  });

  it("normalizes comma separated event lists", () => {
    expect(normalizeCommaSeparatedList("message.received, mailbox.released, message.received, ")).toEqual([
      "message.received",
      "mailbox.released",
    ]);
  });

  it("validates mailbox local part and one time code", () => {
    expect(validateMailboxLocalPart(".bad")).toBe("邮箱前缀仅支持 1-64 位小写字母、数字、点、下划线或短横线，且必须以字母或数字开头。");
    expect(validateMailboxLocalPart("A")).toBeNull();
    expect(validateMailboxLocalPart("ok-mailbox")).toBeNull();
    expect(validateOneTimeCode("12ab")).toBe("验证码必须是 6 位数字。");
    expect(validateOneTimeCode("123456")).toBeNull();
  });

  it("validates selections and integer ranges", () => {
    expect(validateSelection("域名", "", ["1", "2"])).toBe("请选择域名。");
    expect(validateSelection("域名", "3", ["1", "2"])).toBe("域名无效，请重新选择。");
    expect(validateSelection("域名", "1", ["1", "2"])).toBeNull();
    expect(validateIntegerRange("刷新时间", 10, { min: 15, max: 60 })).toBe("刷新时间必须在 15 到 60 之间。");
    expect(validateIntegerRange("刷新时间", 30, { min: 15, max: 60 })).toBeNull();
  });
});

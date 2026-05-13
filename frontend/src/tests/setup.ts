import "@testing-library/jest-dom/vitest";
import { LANGUAGE_STORAGE_KEY } from "../lib/preferences";
import { initializeI18n } from "../lib/i18n";

window.localStorage.setItem(LANGUAGE_STORAGE_KEY, "zh-CN");

if (!HTMLElement.prototype.scrollIntoView) {
  HTMLElement.prototype.scrollIntoView = () => {};
}

if (!window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: query.includes("dark"),
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
}

await initializeI18n();

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { SourceSettingsDialog } from "./SourceSettingsDialog";
import type { Source, SourceSetting } from "../../types";

const source: Source = {
  id: "demo",
  name: "Demo",
  version: "1.0.0",
  abiVersion: 1,
  lang: "en",
  baseUrl: "https://example.test",
  iconUrl: "",
  nsfw: false,
  installedAt: 1,
  loaded: true,
  hasClearance: false,
  hasSettings: true,
};

const settings: SourceSetting[] = [
  { id: "data_saver", title: "Data saver", type: "checkbox", default: false, value: false, hasValue: false },
  {
    id: "mirror",
    title: "Mirror",
    type: "select",
    options: [
      { label: "Primary", value: "a" },
      { label: "Backup", value: "b" },
    ],
    default: "a",
    value: "a",
    hasValue: false,
  },
  {
    id: "base_url",
    title: "Address",
    type: "text",
    placeholder: "https://example.test",
    default: "https://example.test",
    value: "https://example.test",
    hasValue: false,
  },
  { id: "token", title: "Token", type: "text", sensitive: true, hasValue: true },
];

// stubFetch serves the settings list on GET and echoes the posted value back
// as the updated setting on PUT.
function stubFetch() {
  const calls: { url: string; method: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ url, method, body });
      if (method === "PUT") {
        return Response.json({ ...settings[0], hasValue: body?.value !== null, value: body?.value ?? false });
      }
      return Response.json(settings);
    }),
  );
  return calls;
}

describe("SourceSettingsDialog", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders every setting kind and never echoes a sensitive value", async () => {
    stubFetch();
    render(<SourceSettingsDialog source={source} onClose={vi.fn()} />);

    expect(await screen.findByLabelText("Data saver")).not.toBeChecked();
    expect(screen.getByLabelText("Mirror")).toHaveValue("a");
    expect(screen.getByLabelText("Address")).toHaveValue("https://example.test");

    const token = screen.getByLabelText("Token") as HTMLInputElement;
    expect(token.type).toBe("password");
    expect(token.value).toBe("");
    expect(token.placeholder).toBe("Saved");
  });

  it("writes a checkbox change through the settings endpoint", async () => {
    const calls = stubFetch();
    render(<SourceSettingsDialog source={source} onClose={vi.fn()} />);
    fireEvent.click(await screen.findByLabelText("Data saver"));
    await waitFor(() => {
      const put = calls.find((call) => call.method === "PUT");
      expect(put).toBeDefined();
      expect(put?.url).toBe("/api/sources/demo/settings/data_saver");
      expect(put?.body).toEqual({ value: true });
    });
  });

  it("resets a stored value by sending null", async () => {
    const calls = stubFetch();
    render(<SourceSettingsDialog source={source} onClose={vi.fn()} />);
    fireEvent.click(await screen.findByRole("button", { name: /Reset to default/i }));
    await waitFor(() => {
      const put = calls.find((call) => call.method === "PUT");
      expect(put?.body).toEqual({ value: null });
    });
  });

  it("offers a language choice only when the source returns more than one", async () => {
    const calls = stubLanguages();
    const multi: Source = {
      ...source,
      hasSettings: false,
      availableLanguages: ["en", "ja", "pt-br"],
      languages: [],
    };
    const { rerender } = render(<SourceSettingsDialog source={multi} onClose={vi.fn()} />);

    expect(await screen.findByText("Chapter languages")).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("Japanese"));
    await waitFor(() => {
      const put = calls.find((call) => call.method === "PUT");
      expect(put?.url).toBe("/api/sources/demo/languages");
      expect(put?.body).toEqual({ languages: ["ja"] });
    });

    // A single-language source has no language choice to offer.
    rerender(
      <SourceSettingsDialog
        source={{ ...multi, availableLanguages: ["en"] }}
        onClose={vi.fn()}
      />,
    );
    expect(screen.queryByText("Chapter languages")).not.toBeInTheDocument();
  });
});

// stubLanguages serves an empty settings list and echoes the posted language
// selection back as the updated source.
function stubLanguages() {
  const calls: { url: string; method: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ url, method, body });
      if (url.includes("/languages") && method === "PUT") {
        return Response.json({ ...source, languages: body?.languages ?? [] });
      }
      return Response.json([]);
    }),
  );
  return calls;
}

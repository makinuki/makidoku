import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vite-plus/test";
import { CoverImg } from "./CoverImg";
import { MAX_CONCURRENT_COVERS, resetCoverQueue } from "./coverQueue";

describe("CoverImg", () => {
  beforeEach(() => resetCoverQueue());

  it("requests covers within the concurrency limit", async () => {
    const { container } = render(
      <div>
        {Array.from({ length: MAX_CONCURRENT_COVERS + 2 }, (_, index) => (
          <CoverImg key={index} src={`/api/manga/${index}/cover`} />
        ))}
      </div>,
    );
    const started = () =>
      Array.from(container.querySelectorAll("img")).filter((node) => node.getAttribute("src"));
    await waitFor(() => expect(started()).toHaveLength(MAX_CONCURRENT_COVERS));

    fireEvent.load(started()[0]);
    await waitFor(() => expect(started()).toHaveLength(MAX_CONCURRENT_COVERS + 1));
  });

  it("falls back to the placeholder when the request fails", async () => {
    const { container, getByRole } = render(<CoverImg src="/api/manga/1/cover" />);
    await waitFor(() =>
      expect(container.querySelector("img")?.getAttribute("src")).toBe("/api/manga/1/cover"),
    );
    fireEvent.error(container.querySelector("img")!);
    expect(getByRole("img", { name: "No cover available" })).toBeInTheDocument();
  });
});

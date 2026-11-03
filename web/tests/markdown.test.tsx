import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Markdown } from "@/components/Markdown";

describe("Markdown callbacks", () => {
  it("uses the current tag navigation callback after the editor has mounted", () => {
    const first = vi.fn(),
      current = vi.fn();
    const { rerender } = render(
      <Markdown value="Une idée #repere" onTag={first} />,
    );
    rerender(<Markdown value="Une idée #repere" onTag={current} />);
    fireEvent.click(screen.getByText("#repere"));
    expect(current).toHaveBeenCalledWith("repere");
    expect(first).not.toHaveBeenCalled();
    rerender(<Markdown value="Une idée #repere" />);
    fireEvent.click(screen.getByText("#repere"));
    expect(current).toHaveBeenCalledTimes(1);
  });
});

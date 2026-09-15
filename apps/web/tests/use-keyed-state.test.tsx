// @vitest-environment happy-dom
//
// Only the component tests need a DOM; the rest of the suite stays in node,
// which is faster. The pragma is per-file rather than global for that reason.
import { afterEach, expect, test } from "vite-plus/test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

// Testing Library only auto-cleans when the test framework exposes globals.
// The suite imports explicitly, so unmounting between tests is on us —
// without it each render stacks up and queries match several elements.
afterEach(cleanup);
import { useKeyedState } from "../src/lib/use-keyed-state.ts";

/**
 * better-modal keeps a closed modal mounted, so state inside one survives a
 * reopen. These tests stand in for that: the component never unmounts, only
 * its props change — exactly the situation that made the preview modal reopen
 * on the previously viewed file.
 */
function Counter({ subject, start }: { subject: string; start: number }) {
  const [value, setValue] = useKeyedState(subject, start);
  return (
    <div>
      <span data-testid="value">{value}</span>
      <button type="button" onClick={() => setValue((v) => v + 1)}>
        bump
      </button>
      <button type="button" onClick={() => setValue(99)}>
        set
      </button>
    </div>
  );
}

const value = () => screen.getByTestId("value").textContent;

test("keeps its value while the key is unchanged", () => {
  const { rerender } = render(<Counter subject="a" start={0} />);
  fireEvent.click(screen.getByText("bump"));
  expect(value()).toBe("1");

  // A re-render with the same key must not discard what the user did.
  rerender(<Counter subject="a" start={0} />);
  expect(value()).toBe("1");
});

test("resets when the key changes, without unmounting", () => {
  const { rerender } = render(<Counter subject="a" start={0} />);
  fireEvent.click(screen.getByText("bump"));
  fireEvent.click(screen.getByText("bump"));
  expect(value()).toBe("2");

  // This is the regression: reopening for a different subject must start
  // fresh rather than carry the previous subject's state over.
  rerender(<Counter subject="b" start={0} />);
  expect(value()).toBe("0");
});

test("adopts the new initial value on a key change", () => {
  const { rerender } = render(<Counter subject="a" start={3} />);
  expect(value()).toBe("3");

  rerender(<Counter subject="b" start={7} />);
  expect(value()).toBe("7");
});

test("accepts a direct value as well as an updater", () => {
  render(<Counter subject="a" start={0} />);
  fireEvent.click(screen.getByText("set"));
  expect(value()).toBe("99");
});

test("an updater after a key change builds on the new initial value", () => {
  const { rerender } = render(<Counter subject="a" start={0} />);
  fireEvent.click(screen.getByText("bump"));
  expect(value()).toBe("1");

  rerender(<Counter subject="b" start={10} />);
  fireEvent.click(screen.getByText("bump"));
  expect(value()).toBe("11");
});

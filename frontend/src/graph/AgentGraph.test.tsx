import { render } from "@solidjs/testing-library";
import { describe, it, expect } from "vitest";
import AgentGraph from "./AgentGraph";

describe("AgentGraph", () => {
  it("renders an svg element", () => {
    const { container } = render(() => <AgentGraph />);
    const svg = container.querySelector("svg");
    expect(svg).toBeDefined();
  });
});

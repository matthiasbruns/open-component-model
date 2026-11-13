import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { DOCKLE_CHECKS, addDockle, ruleVerdict, validateModule } from "./bsi-report.js";

// Checkpoints dockle v0.4.15 (the DOCKLE_IMAGE pinned in .env) implements:
// https://github.com/goodwithtech/dockle/blob/v0.4.15/pkg/types/checkpoint.go
// CHECKPOINT.md lists CIS-DI-0002..0004 too, but as "Not supported" or folded
// into CIS-DI-0005, so dockle never reports them.
const IMPLEMENTED = [
    "CIS-DI-0001", "CIS-DI-0005", "CIS-DI-0006", "CIS-DI-0007", "CIS-DI-0008", "CIS-DI-0009", "CIS-DI-0010",
    "DKL-DI-0001", "DKL-DI-0002", "DKL-DI-0003", "DKL-DI-0004", "DKL-DI-0005", "DKL-DI-0006",
    "DKL-LI-0001", "DKL-LI-0002", "DKL-LI-0003",
];

describe("dockle checks dockle never runs", () => {
    it("DOCKLE_CHECKS lists only checkpoints dockle implements", () => {
        assert.deepEqual([...DOCKLE_CHECKS].sort(), [...IMPLEMENTED].sort());
    });

    it("rejects a mapping to CIS-DI-0003 (Not supported by dockle)", () => {
        const module = { module: "SYS.1.6", title: "t", edition: "2022", source: "s",
            controls: [{ id: "SYS.1.6.A6", title: "t", level: "basic", ocm: "o", status: "automated", rules: ["dockle:CIS-DI-0003"] }] };
        assert.match(validateModule(module).join(), /unknown dockle check/);
    });

    it("does not report a never-run checkpoint as passed", () => {
        const results = new Map();
        addDockle(results, { summary: {}, details: [] }, "cli-amd64");
        assert.notEqual(ruleVerdict(results.get("dockle:CIS-DI-0003")).verdict, "pass");
    });
});

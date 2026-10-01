import { describe, expect, test } from "vitest";

import type { Channel } from "../../types";
import { CHANNEL_TYPE_VLLM } from "../../constants";
import {
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from "../channel-form";

function channelWithSettings(
  type: number,
  settings: Record<string, unknown>,
): Channel {
  return {
    id: 58,
    type,
    key: "redacted",
    name: "Custom upstream",
    status: 1,
    models: "demo-model",
    group: "default",
    base_url: "https://upstream.example",
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: "random",
    },
    settings: JSON.stringify(settings),
  } as Channel;
}

describe("advanced custom channel form", () => {
  test("loads saved routes into the editor field and writes edits back", () => {
    const channel = channelWithSettings(58, {
      tool_loss_policy: "keep",
      advanced_custom: {
        advanced_routes: [
          {
            incoming_path: "/v1/chat/completions",
            upstream_path: "/v1/chat/completions",
            converter: "none",
          },
        ],
      },
    });

    const defaults = transformChannelToFormDefaults(channel);
    const loaded = JSON.parse(defaults.advanced_custom || "{}");
    expect(loaded.advanced_routes[0].upstream_path).toBe(
      "/v1/chat/completions",
    );

    const edited = JSON.parse(defaults.advanced_custom || "{}");
    edited.advanced_routes[0].upstream_path = "/custom/chat";
    const payload = transformFormDataToUpdatePayload(
      { ...defaults, advanced_custom: JSON.stringify(edited) },
      channel.id,
    );
    const settings = JSON.parse(String(payload.settings));

    expect(settings.advanced_custom.advanced_routes[0].upstream_path).toBe(
      "/custom/chat",
    );
    expect(settings.tool_loss_policy).toBe("keep");
  });

  test("does not copy an editor value onto a named preset channel", () => {
    const channel = channelWithSettings(CHANNEL_TYPE_VLLM, {
      tool_loss_policy: "drop",
    });
    const defaults = transformChannelToFormDefaults(channel);
    const payload = transformFormDataToUpdatePayload(
      {
        ...defaults,
        advanced_custom: JSON.stringify({
          advanced_routes: [{ incoming_path: "/v1/models" }],
        }),
      },
      channel.id,
    );
    const settings = JSON.parse(String(payload.settings));

    expect(settings.advanced_custom).toBeUndefined();
    expect(settings.tool_loss_policy).toBe("drop");
  });
});

/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from "node:assert/strict";

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, test, vi } from "vitest";

import { api } from "@/lib/api";

import { OAuthProviders } from "../components/oauth-providers";
import {
  getOAuthSessionStorage,
  resolveOAuthLoginRedirectTarget,
} from "../lib/oauth-callback-mode";
import type { SystemStatus } from "../types";

vi.mock("./use-auth-redirect", () => ({
  useAuthRedirect: () => ({
    handleLoginSuccess: vi.fn(),
    redirectTo2FA: vi.fn(),
    redirectToLogin: vi.fn(),
    redirectToRegister: vi.fn(),
  }),
}));

describe("useOAuthLogin", () => {
  test("stores the login redirect before leaving for an OAuth provider", async () => {
    const postSpy = vi.spyOn(api, "post").mockImplementation(async (url) => {
      if (url === "/api/user/auth/logout") {
        return { data: { success: true, message: "" } };
      }
      if (url === "/api/oauth/state") {
        return { data: { success: true, data: { flow_token: "login-state" } } };
      }
      throw new Error(`unexpected post ${String(url)}`);
    });
    const openSpy = vi.spyOn(window, "open").mockImplementation(() => null);
    const status = {
      github_oauth: true,
      github_client_id: "github-client",
    } as SystemStatus;

    render(
      <OAuthProviders
        status={status}
        redirectTo="/keys?tab=default#active"
        affiliateCode="INVITE-1"
      />,
    );

    await userEvent.click(
      screen.getByRole("button", { name: /Continue with GitHub/i }),
    );

    await waitFor(() => assert.equal(openSpy.mock.calls.length, 1));
    assert.equal(postSpy.mock.calls.length, 2);
    assert.equal(
      resolveOAuthLoginRedirectTarget(
        getOAuthSessionStorage(window),
        "github",
        "login-state",
        undefined,
        window.location.origin,
      ),
      "/keys?tab=default#active",
    );
  });
});

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

import { render } from "@testing-library/react";
import { describe, test, vi } from "vitest";

import type { SystemStatus } from "../types";
import { OAuthProviders } from "./oauth-providers";

const mocks = vi.hoisted(() => ({
  useOAuthLogin: vi.fn(() => ({
    isLoading: false,
    githubButtonText: "",
    githubButtonDisabled: false,
    handleGitHubLogin: vi.fn(),
    handleDiscordLogin: vi.fn(),
    handleOIDCLogin: vi.fn(),
    handleLinuxDOLogin: vi.fn(),
    handleTelegramLogin: vi.fn(),
    handleCustomOAuthLogin: vi.fn(),
  })),
}));

vi.mock("../hooks/use-oauth-login", () => ({
  useOAuthLogin: mocks.useOAuthLogin,
}));

describe("OAuthProviders", () => {
  test("passes redirect and affiliate code as separate OAuth login options", () => {
    const status = {
      github_oauth: true,
      github_client_id: "github-client",
    } as SystemStatus;

    render(
      <OAuthProviders
        status={status}
        redirectTo="/dashboard"
        affiliateCode="INVITE-1"
      />,
    );

    assert.equal(mocks.useOAuthLogin.mock.calls.length, 1);
    assert.deepEqual(mocks.useOAuthLogin.mock.calls[0], [
      status,
      { redirectTo: "/dashboard", affiliateCode: "INVITE-1" },
    ]);
  });
});

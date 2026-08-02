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

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, test, vi } from "vitest";

import { ROLE } from "@/lib/roles";
import { useAuthStore } from "@/stores/auth-store";

import { ChannelsPrimaryButtons } from "./channels-primary-buttons";

const mocks = vi.hoisted(() => ({
  handleDeleteAllDisabled: vi.fn(),
  handleFixAbilities: vi.fn(),
  handleTestAllChannels: vi.fn(),
  handleUpdateAllBalances: vi.fn(),
  useChannels: vi.fn(() => ({
    setOpen: vi.fn(),
    setCurrentRow: vi.fn(),
    enableTagMode: false,
    setEnableTagMode: vi.fn(),
    idSort: false,
    setIdSort: vi.fn(),
    batchMode: false,
    setBatchMode: vi.fn(),
    upstream: {
      detectAllLoading: false,
      applyAllLoading: false,
      detectAllUpdates: vi.fn(),
      applyAllUpdates: vi.fn(),
    },
  })),
}));

vi.mock("../lib", () => ({
  handleDeleteAllDisabled: mocks.handleDeleteAllDisabled,
  handleFixAbilities: mocks.handleFixAbilities,
  handleTestAllChannels: mocks.handleTestAllChannels,
  handleUpdateAllBalances: mocks.handleUpdateAllBalances,
}));

vi.mock("./channels-provider", () => ({
  useChannels: mocks.useChannels,
}));

function renderButtons() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <ChannelsPrimaryButtons />
    </QueryClientProvider>,
  );
}

describe("ChannelsPrimaryButtons", () => {
  test("disables repair consistency when the admin lacks channel operate permission", async () => {
    useAuthStore.getState().auth.setUser({
      id: 1,
      username: "limited-admin",
      role: ROLE.ADMIN,
      permissions: {
        admin_permissions: {
          channel: {
            read: true,
            operate: false,
            write: false,
            sensitive_write: false,
          },
        },
      },
    });

    renderButtons();
    await userEvent.click(screen.getAllByRole("button")[1]);

    const repairItem = await screen.findByText("Repair Channel Consistency");
    const menuItem = repairItem.closest('[data-slot="dropdown-menu-item"]');
    assert.ok(menuItem);
    assert.equal(menuItem?.hasAttribute("data-disabled"), true);

    await userEvent.click(repairItem);

    assert.equal(
      screen.queryByText("Repair channel consistency?"),
      null,
    );
    assert.equal(mocks.handleFixAbilities.mock.calls.length, 0);
  });
});

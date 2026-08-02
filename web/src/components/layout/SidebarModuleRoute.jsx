/*
Copyright (C) 2025 QuantumNous

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

import React from 'react';
import { Navigate } from 'react-router-dom';
import { Spin } from '@douyinfe/semi-ui';
import { useSidebar } from '../../hooks/common/useSidebar';

export default function SidebarModuleRoute({
  section,
  module,
  children,
  redirectTo = '/forbidden',
}) {
  const { loading, isModuleVisible } = useSidebar();

  if (loading) {
    return (
      <div className='mt-[120px] flex justify-center'>
        <Spin size='large' />
      </div>
    );
  }

  if (!isModuleVisible(section, module)) {
    return <Navigate to={redirectTo} replace />;
  }

  return children;
}

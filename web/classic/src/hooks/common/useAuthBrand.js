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

import { useContext, useEffect, useMemo, useState } from 'react';
import {
  API,
  applySystemBrandToDom,
  getLogo,
  getSystemName,
  setStatusData,
} from '../../helpers';
import { StatusContext } from '../../context/Status';

export function useAuthBrand() {
  const [statusState, statusDispatch] = useContext(StatusContext);
  const [brand, setBrand] = useState(() => ({
    logo: getLogo(),
    systemName: getSystemName(),
  }));

  useEffect(() => {
    let disposed = false;

    const syncBrand = (data) => {
      if (!data || disposed) return;
      setStatusData(data);
      statusDispatch({ type: 'set', payload: data });
      applySystemBrandToDom({
        systemName: data.system_name,
        logo: data.logo,
      });
      setBrand({
        logo: getLogo(),
        systemName: getSystemName(),
      });
    };

    if (statusState?.status) {
      syncBrand(statusState.status);
      return () => {
        disposed = true;
      };
    }

    API.get('/api/status')
      .then((res) => {
        const { success, data } = res.data || {};
        if (success) syncBrand(data);
      })
      .catch(() => {
        applySystemBrandToDom();
      });

    return () => {
      disposed = true;
    };
  }, [statusDispatch, statusState?.status]);

  return useMemo(() => brand, [brand]);
}

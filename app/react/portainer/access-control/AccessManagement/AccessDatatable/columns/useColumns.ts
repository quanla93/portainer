import _ from 'lodash';
import { useMemo } from 'react';

import { inheritedName } from './inheritedName';
import { name } from './name';
import { type } from './type';
import { role } from './role';

export function useColumns({
  showRoles,
  inheritFrom,
}: {
  showRoles: boolean;
  inheritFrom: boolean;
}) {
  return useMemo(
    () =>
      _.compact([
        inheritFrom ? inheritedName : name,
        type,
        showRoles && role,
      ]),
    [inheritFrom, showRoles]
  );
}

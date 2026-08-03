import { SchemaOf, array, boolean, number, object, ref, string } from 'yup';
import { useMemo } from 'react';

import { usePublicSettings } from '@/react/portainer/settings/queries';
import { AuthenticationMethod } from '@/react/portainer/settings/types';
import { useUsers } from '@/portainer/users/queries';

import { FormValues } from './FormValues';

export function useValidation(): SchemaOf<FormValues> {
  const usersQuery = useUsers(true);
  const settingsQuery = usePublicSettings();

  const authMethod =
    settingsQuery.data?.AuthenticationMethod ?? AuthenticationMethod.Internal;

  return useMemo(() => {
    const users = usersQuery.data ?? [];

    const base = object({
      username: string()
        .required('Username is required')
        .test({
          name: 'unique',
          message: 'Username is already taken',
          test: (value) => users.every((u) => u.Username !== value),
        }),
      isLocal: boolean().default(true),
      password: string().default(''),
      confirmPassword: string().default(''),
      isAdmin: boolean().default(false),
      teams: array(number().required()).required(),
    });

    if (authMethod === AuthenticationMethod.Internal) {
      return base.concat(
        passwordValidation(settingsQuery.data?.RequiredPasswordLength, true)
      );
    }

    return base.concat(
      passwordValidation(settingsQuery.data?.RequiredPasswordLength, false)
    );
  }, [authMethod, settingsQuery.data?.RequiredPasswordLength, usersQuery.data]);
}

function passwordValidation(
  minLength: number | undefined = 12,
  isRequired = true
) {
  return object({
    password: isRequired
      ? string()
          .required('Password is required')
          .min(
            minLength,
            ({ value, min }) =>
              `The password must be at least ${min} characters long. (${value.length}/${min})`
          )
      : string()
          .optional()
          .default('')
          .test({
            name: 'minLength',
            message: `The password must be at least ${minLength} characters long.`,
            test: (value) => !value || value.length >= (minLength ?? 12),
          }),
    confirmPassword: isRequired
      ? string()
          .required('Confirm password is required')
          .oneOf([ref('password'), null], 'Passwords must match')
      : string()
          .default('')
          .test({
            name: 'match',
            message: 'Passwords must match',
            test: function (value) {
              return !this.parent.password || value === this.parent.password;
            },
          }),
  });
}

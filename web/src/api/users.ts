// SPDX-License-Identifier: Apache-2.0
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { apiRequest } from './client';
import type {
  ChangePasswordRequest,
  CreateUserRequest,
  UpdateUserRequest,
  UserResponse,
  UsersResponse,
} from './types.gen';

export const userKeys = { all: ['users'] as const };

/** useUsers lists every user, for admins. */
export function useUsers() {
  return useQuery({
    queryKey: userKeys.all,
    queryFn: () => apiRequest<UsersResponse>('api/users'),
  });
}

/** useUserChange runs a change to users, then refreshes the list. */
function useUserChange<T, R>(change: (input: T) => Promise<R>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: change,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: userKeys.all }),
  });
}

export function useCreateUser() {
  return useUserChange((request: CreateUserRequest) =>
    apiRequest<UserResponse>('api/users', { method: 'POST', body: request }),
  );
}

export function useUpdateUser() {
  return useUserChange(({ id, ...request }: UpdateUserRequest & { id: number }) =>
    apiRequest<UserResponse>(`api/users/${String(id)}`, { method: 'PATCH', body: request }),
  );
}

/** useResetPassword sets another user's password, for an admin. */
export function useResetPassword() {
  return useUserChange(({ id, password }: { id: number; password: string }) =>
    apiRequest<undefined>(`api/users/${String(id)}/password`, {
      method: 'POST',
      body: { password },
    }),
  );
}

/** useChangePassword changes the signed-in user's own password. */
export function useChangePassword() {
  return useMutation({
    mutationFn: (request: ChangePasswordRequest) =>
      apiRequest<undefined>('api/auth/password', { method: 'POST', body: request }),
  });
}

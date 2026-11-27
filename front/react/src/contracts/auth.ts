// Contrato de sesión y permisos (PLAN §4.2, §4.4).

export interface AuthContext {
  subjectId: string;
  subjectName: string;
  isGlobalAdmin: boolean;
  permissions: string[];
  effectiveOrganizationIds: string[];
}

export interface Session {
  status: 'anonymous' | 'must-change-password' | 'authenticated';
  context?: AuthContext;
  can(permission: string): boolean;
}

import { z } from "zod";
import i18n from "@/lib/i18n";

function t(key: string, options?: Record<string, unknown>) {
  return i18n.t(key, options);
}

export const loginSchema = z.object({
  login: z.string().min(1, { error: () => t("validation.required") }),
  password: z.string().min(1, { error: () => t("validation.required") }),
});

export const registerSchema = z.object({
  username: z
    .string()
    .min(1, { error: () => t("validation.required") })
    .min(2, { error: () => t("validation.minLength", { min: 2 }) })
    .max(32, { error: () => t("validation.maxLength", { max: 32 }) }),
  email: z
    .string()
    .min(1, { error: () => t("validation.required") })
    .email({ error: () => t("validation.emailInvalid") }),
  password: z
    .string()
    .min(1, { error: () => t("validation.required") })
    .min(8, { error: () => t("validation.minLength", { min: 8 }) }),
});

export const resetPasswordSchema = z.object({
  code: z
    .string()
    .min(1, { error: () => t("validation.required") })
    .regex(/^\d{6}$/, { error: () => t("validation.codeFormat") }),
  newPassword: z
    .string()
    .min(1, { error: () => t("validation.required") })
    .min(8, { error: () => t("validation.minLength", { min: 8 }) }),
});

export const forgotPasswordSchema = z.object({
  login: z.string().min(1, { error: () => t("validation.required") }),
});

export const twoFactorSchema = z.object({
  code: z
    .string()
    .min(1, { error: () => t("validation.required") })
    .regex(/^\d{6}$/, { error: () => t("validation.codeFormat") }),
});

export const mailboxLocalPartSchema = z
  .string()
  .regex(/^$|^[a-z0-9][a-z0-9._-]{1,63}$/, { error: () => t("validation.mailboxLocalPart") });

export type LoginFormData = z.infer<typeof loginSchema>;
export type RegisterFormData = z.infer<typeof registerSchema>;
export type ResetPasswordFormData = z.infer<typeof resetPasswordSchema>;
export type ForgotPasswordFormData = z.infer<typeof forgotPasswordSchema>;
export type TwoFactorFormData = z.infer<typeof twoFactorSchema>;

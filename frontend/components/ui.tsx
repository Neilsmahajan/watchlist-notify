"use client";

import { ReactNode } from "react";
import { useTheme } from "@/components/ThemeProvider";

function classNames(
  ...classes: Array<string | false | null | undefined>
): string {
  return classes.filter(Boolean).join(" ");
}

interface LoadingSpinnerProps {
  size?: "sm" | "md" | "lg";
  className?: string;
}

export function LoadingSpinner({
  size = "md",
  className = "",
}: LoadingSpinnerProps) {
  const sizeClasses = {
    sm: "w-4 h-4",
    md: "w-8 h-8",
    lg: "w-12 h-12",
  };

  return (
    <span
      role="status"
      aria-live="polite"
      aria-busy="true"
      className={`inline-block animate-spin rounded-full border-2 border-gray-300 border-t-blue-600 ${sizeClasses[size]} ${className} dark:border-slate-600 dark:border-t-blue-400`}
    />
  );
}

interface EmptyStateProps {
  title: string;
  description: string;
  action?: ReactNode;
  icon?: ReactNode;
}

export function EmptyState({
  title,
  description,
  action,
  icon,
}: EmptyStateProps) {
  return (
    <div className="text-center py-12">
      {icon && (
        <div className="mx-auto w-12 h-12 text-gray-400 mb-4 dark:text-slate-500">
          {icon}
        </div>
      )}
      <h3 className="text-lg font-medium text-gray-900 mb-2 dark:text-slate-100">
        {title}
      </h3>
      <p className="text-gray-500 mb-6 dark:text-slate-400">{description}</p>
      {action}
    </div>
  );
}

interface ThemedCardProps {
  children: ReactNode;
  className?: string;
}

export function ThemedCard({ children, className = "" }: ThemedCardProps) {
  const { theme } = useTheme();
  const themeClasses =
    theme === "dark"
      ? "border-slate-700 bg-slate-900 hover:bg-slate-800"
      : "border-gray-200 bg-white hover:bg-gray-50";

  return (
    <div
      className={classNames(
        "rounded-lg border p-4 transition-colors",
        themeClasses,
        className
      )}
    >
      {children}
    </div>
  );
}

interface ButtonProps {
  children: ReactNode;
  variant?: "primary" | "secondary" | "outline";
  size?: "sm" | "md" | "lg";
  disabled?: boolean;
  loading?: boolean;
  onClick?: () => void;
  href?: string;
  className?: string;
  type?: "button" | "submit" | "reset";
}

export function Button({
  children,
  variant = "primary",
  size = "md",
  disabled = false,
  loading = false,
  onClick,
  href,
  className = "",
  type,
}: ButtonProps) {
  const baseClasses =
    "inline-flex items-center justify-center font-medium rounded-lg transition-colors focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-offset-white dark:focus:ring-offset-slate-950";

  const variantClasses = {
    primary:
      "bg-blue-600 text-white hover:bg-blue-700 focus:ring-blue-500 dark:bg-blue-500 dark:hover:bg-blue-400 dark:focus:ring-blue-400",
    secondary:
      "bg-gray-200 text-gray-900 hover:bg-gray-300 focus:ring-gray-500 dark:bg-slate-700 dark:text-slate-100 dark:hover:bg-slate-600 dark:focus:ring-slate-500",
    outline:
      "border border-gray-300 bg-white text-gray-900 hover:bg-gray-100 focus:ring-blue-500 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-100 dark:hover:bg-slate-800",
  };

  const sizeClasses = {
    sm: "px-3 py-1.5 text-sm",
    md: "px-4 py-2 text-sm",
    lg: "px-6 py-3 text-base",
  };

  const classes = `${baseClasses} ${variantClasses[variant]} ${
    sizeClasses[size]
  } ${className} ${disabled || loading ? "opacity-50 cursor-not-allowed" : ""}`;

  if (href) {
    return (
      <a href={href} className={classes}>
        {loading && <LoadingSpinner size="sm" className="mr-2" />}
        {children}
      </a>
    );
  }

  return (
    <button
      onClick={onClick}
      disabled={disabled || loading}
      type={type}
      className={classes}
    >
      {loading && <LoadingSpinner size="sm" className="mr-2" />}
      {children}
    </button>
  );
}

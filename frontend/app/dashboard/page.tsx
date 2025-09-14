"use client";

import { useUser } from "@auth0/nextjs-auth0";
import { LoadingSpinner } from "@/components/ui";
import { redirect } from "next/navigation";

export default function Dashboard() {
  const { user, isLoading } = useUser();

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <LoadingSpinner size="lg" />
      </div>
    );
  }

  if (!user) {
    redirect("/auth/login");
  }

  // This content is the same as the logged-in state of the home page
  // In the future, you might want to differentiate between "/" and "/dashboard"
  redirect("/");

  return null;
}

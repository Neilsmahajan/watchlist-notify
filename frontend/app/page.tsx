export default function Home() {
  return (
    <div className="font-sans grid grid-rows-[20px_1fr_20px] items-center justify-items-center min-h-screen p-8 pb-20 gap-16 sm:p-20">
      <main className="flex flex-col gap-6 row-start-2 items-center sm:items-start">
        <h1 className="text-5xl font-extrabold tracking-tight sm:text-[5rem]">
          Watchlist Notify
        </h1>
        <div className="flex gap-4">
          <a className="underline" href="/auth/login">
            Log in
          </a>
          <a className="underline" href="/auth/logout">
            Log out
          </a>
          <a className="underline" href="/api/session">
            View session
          </a>
          <a className="underline" href="/api/me">
            Call /api/me
          </a>
        </div>
      </main>
    </div>
  );
}

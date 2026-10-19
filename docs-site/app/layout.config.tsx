import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';

export const baseOptions: BaseLayoutProps = {
  nav: {
    title: (
      <span className="inline-flex items-center gap-2 font-semibold">
        <img
          src="/decathlon-cli-logo.svg"
          alt=""
          width={32}
          height={32}
          className="h-7 w-7"
        />
        <span>decathlon-cli</span>
      </span>
    ),
  },
  githubUrl: 'https://github.com/n0nuser/decathlon-cli',
};

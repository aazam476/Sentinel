// SPDX-License-Identifier: Apache-2.0
import type { CreateEnrollmentTokenResponse } from '../../api/types.gen';
import { CopyButton } from '../../components/CopyButton/CopyButton';
import { formatDateTime } from '../../formatting/formatting';
import styles from './AddMachinePage.module.css';
import { installCommands, serverURL } from './buildInstallCommands';

interface InstallCommandsProps {
  created: CreateEnrollmentTokenResponse;
}

/**
 * InstallCommands shows the commands for a new token. The token is in them
 * and is never shown again, so this says so plainly.
 */
export function InstallCommands({ created }: InstallCommandsProps) {
  const commands = installCommands(created, serverURL(document.baseURI));
  const uses =
    created.token.maxUses === 1 ? 'one machine' : `up to ${String(created.token.maxUses)} machines`;
  return (
    <section className={styles.commands} aria-labelledby="install-title">
      <h2 id="install-title" className={styles.sectionTitle}>
        Run this on the machine
      </h2>
      <p className={styles.text}>
        Copy the sentinel-agent program onto the machine first. The command works for {uses} until{' '}
        {formatDateTime(created.token.expiresAt)}, and won’t be shown again.
      </p>
      {commands.map((command) => (
        <div key={command.system} className={styles.command}>
          <h3 className={styles.system}>{command.system}</h3>
          <p className={styles.text}>{command.where}</p>
          <div className={styles.codeRow}>
            <code className={styles.code}>{command.command}</code>
            <CopyButton text={command.command} label={`${command.system} command`} />
          </div>
        </div>
      ))}
      <p className={styles.text}>The machine appears in the fleet as soon as its agent connects.</p>
    </section>
  );
}

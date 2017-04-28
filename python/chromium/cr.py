import stonesthrow
import argparse
import os
import platform
import shutil
import subprocess
import time


def Argument(*args, **kwargs):
    """Decorator Argument adds an annotation to a function indicating 
  which argparse arguments are required by the underlying command.
  
  Usage:
    @Argument('--hello', '-H', help='Hello world!')
    def foo():
      pass

  ... will annotate `foo` with an argument that's equivalent to what's produced
  by argparse.add_argument().
  """

    def wrap(func):
        if 'option_arguments' not in vars(func):
            vars(func)['option_arguments'] = []
        vars(func)['option_arguments'].append((args, kwargs))
        return func

    return wrap


def CommandNeedsSource(func):
    vars(func)['needs_source'] = True
    return func


def InvokeMb(options, *args):
    if len(args) == 0 or not isinstance(options, stonesthrow.Options):
        raise ValueError('first argument should be an Options object')
    if platform.system() == 'Windows':
        mb_tool = os.path.join(options.source_path, 'tools', 'mb', 'mb.bat')
    else:
        mb_tool = os.path.join(options.source_path, 'tools', 'mb', 'mb.py')
    command = [
        mb_tool, args[0], '-c', options.mb_config, '-g', options.goma_path,
        options.build_path
    ] + list(args[1:])
    stonesthrow.CheckCall(command, cwd=options.source_path)


def IsGomaRunning(options, goma_status_cmd):
    output = stonesthrow.CheckOutput(goma_status_cmd)
    for line in output.splitlines():
        line = line.strip()
        if line.startswith('compiler proxy '
                           ) and ' status: ' in line and line.endswith('ok'):
            return True
    return False


def EnsureGoma(options):
    if platform.system() == 'Windows':
        attempted_to_start_goma = False
        for x in range(5):
            goma_command = os.path.join(options.goma_path, 'goma_ctl.bat')
            if IsGomaRunning(options,
                             ['cmd.exe', '/c', goma_command, 'status']):
                return True

            if not attempted_to_start_goma:
                attempted_to_start_goma = True
                command = ['cmd.exe', '/c', goma_command, 'ensure_start']
                # Don't wait for completion.
                subprocess.Popen(command, shell=True)

                time.sleep(1)
        stonesthrow.Error('timed out while attempting to start Goma')
        return False

    # On Posix
    goma_script = os.path.join(options.goma_path, 'goma_ctl.py')
    if IsGomaRunning(options, ['python', goma_script, 'status']):
        return True

    stonesthrow.CheckCall(['python', goma_script, 'ensure_start'])
    return True


def GetNinjaCommand(options):
    command = ['ninja']
    if options.max_build_jobs != 0:
        command += ['-j', str(options.max_build_jobs)]
    command += ['-C', options.build_path]
    return command


def _BuildTargetFromCommand(options, command):
    if os.path.isabs(command):
        return None

    if os.path.dirname(command) in ['', '.']:
        return os.path.basename(command)

    return None


class Commands:
    def Prepare_Command(self, options):
        """prepare build directory."""

        InvokeMb(options, 'gen')

    @CommandNeedsSource
    @Argument(
        'targets',
        nargs=argparse.REMAINDER,
        metavar='TARGETS',
        help='targets to build')
    def Build_Command(self, options):
        """build specified targets."""

        if len(options.targets) == 0:
            stonesthrow.Error('no targets specified')
            return

        # If Goma fails, don't try to run a build. A suitable error should
        # already have been presented.
        if not EnsureGoma(options):
            return

        stonesthrow.CheckCall(
            GetNinjaCommand(options) + options.targets, cwd=options.build_path)

    @Argument(
        'targets',
        nargs=argparse.REMAINDER,
        metavar='TARGETS',
        help='targets to clean')
    def Clean_Command(self, options):
        """clean specified targets."""

        stonesthrow.CheckCall(
            GetNinjaCommand(options) + ['-t', 'clean'] + options.targets,
            cwd=build_path)

    @Argument('--source', action='store_true', help='clobber source')
    @Argument('--output', '-o', action='store_true', help='clobber output')
    @Argument('--force', '-f', action='store_true', help='force')
    def Clobber_Command(self, options):
        """clobber output directory."""

        if not (options.source or options.output):
            stonesthrow.Info('need to specify either --source or --output')
            return

        if options.source:
            force_flag = ['--force'] if options.force else []
            stonesthrow.CheckCall(['git', 'clean'] + foce_flag)

        if options.output:
            if options.force:
                shutil.rmtree(options.build_path)
                InvokeMb(options, 'gen')
            else:
                stonesthrow.Info(
                    'will remove everything in {}'.format(options.build_path))

    @Argument(
        '--build',
        action='store_true',
        dest='build',
        help='build dependencies')
    @Argument(
        'args',
        nargs=argparse.REMAINDER,
        metavar="ARGUMENTS",
        help="command to run")
    def Run_Command(self, options):
        """runs a command."""

        if len(options.args) == 0:
            stonesthrow.Error('no arguments specified')
            return

        if options.build:
            build_target = _BuildTargetFromCommand(options, options.args[0])
            if build_target is not None:
                ninja_command = GetNinjaCommand(options)
                stonesthrow.CheckCall(ninja_command + [build_target])

        stonesthrow.CheckCall(options.args, cwd=options.build_path)


    def RebaseUpdate_Command(self, options):
        """runs 'git rebase-update'."""

        clank_dir = os.path.join(options.source_path, "clank")
        if os.path.exists(clank_dir):
            stonesthrow.CheckCall(['git', 'checkout', 'origin/master'], cwd=clank_dir)
            stonesthrow.CheckCall(['git', 'pull', 'origin', 'master'], cwd=clank_dir)

        chrome_dir = options.source_path
        stonesthrow.CheckCall(['git', 'checkout', 'origin/master'], cwd=chrome_dir)
        stonesthrow.CheckCall(['git', 'checkout', 'origin', 'master'], cwd=chrome_dir)

        stonesthrow.CheckCall(['gclient', 'sync'], cwd=chrome_dir)

        stonesthrow.CheckCall(['git', 'clean', '-f'], cwd=chrome_dir)
        stonesthrow.CheckCall(['git', 'rebase-update', '--no-fetch', '--keep-going'], cwd=chrome_dir)


def ConfigureFlags(config):
    parser = argparse.ArgumentParser(
        description='Chromium platform specific subcommands')
    subparsers = parser.add_subparsers()

    c = Commands()

    for name in dir(c):
        if not name.endswith('_Command'):
            continue

        value = getattr(c, name)

        command = name[:-len('_command')].lower()
        doc = value.__doc__
        subparser = subparsers.add_parser(command, help=value.__doc__)
        subparser.set_defaults(method=value)

        if hasattr(value, 'option_arguments'):
            for args, kwargs in value.option_arguments:
                subparser.add_argument(*args, **kwargs)

    return parser


def NeedsSource(options):
    if not hasattr(options, 'method'):
        raise ValueError('invalid command')
    return hasattr(options.method,
                   'needs_source') and options.method.needs_source


def _GetCommandDescriptor(command_name, command):
    doc = command.__doc__.splitlines()
    description = doc[0]
    depends_on_source = hasattr(command, 'needs_source') and command.needs_source

    usage = '\n'.join(doc[2:])

    if hasattr(command, 'option_arguments'):
        usage += "\nOptions:\n"
        for args, kwargs in command.option_arguments:
            usage += """{flags} :
    {help}
""".format(flags=', '.join(args),
           help=kwargs.get("help", "(no description given)"))

    return {
        "name": [command_name],
        "description": description,
        "usage": usage,
        "depends_on_source": depends_on_source,
        "visible": True
    }


def ListCommands(options):
    commands = []

    c = Commands()
    for name in dir(c):
        if not name.endswith('_Command'):
            continue

        commands.append(
            _GetCommandDescriptor(name[:-len('_command')].lower(),
                                  getattr(c, name)))

    return commands


def Run(options):
    if not hasattr(options, 'method'):
        raise ValueError('invalid command')
    return options.method(options)

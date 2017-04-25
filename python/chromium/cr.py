import stonesthrow
import argparse
import os
import platform
import subprocess
import time

def Argument(*args, **kwargs):
    def wrap(func):
        if 'option_arguments' not in vars(func):
            vars(func)['option_arguments'] = []
        vars(func)['option_arguments'].append((args, kwargs))
        return func
    return wrap

def InvokeMb(options, *args):
    if len(args) == 0 or not isinstance(options, stonesthrow.Options):
        raise ValueError("first argument should be an Options object")
    if platform.system() == "Windows":
        mb_tool = os.path.join(options.source_path, "tools", "mb", "mb.bat")
    else:
        mb_tool = os.path.join(options.source_path, "tools", "mb", "mb.py")
    command = [mb_tool, args[0], "-c", options.mb_config, "-g", options.goma_path, options.build_path] + list(args[1:])
    stonesthrow.CheckCall(command)

def IsGomaRunning(options, cmd):
    pass

def EnsureGoma(options):
    if platform.system() == "Windows":
        attempted_to_start_goma = False
        for x in range(5):
            goma_command = os.path.join(options.goma_path, 'goma_ctl.bat')
            if IsGomaRunning(options, ['cmd.exe', '/c', goma_command, 'status']):
                return True

            if not attempted_to_start_goma:
                attempted_to_start_goma = True
                command = ['cmd.exe', '/c', goma_command, 'ensure_start']
                # Don't wait for completion.
                subprocess.Popen(command, shell=True)

                time.sleep(1)
        stonesthrow.Error("timed out while attempting to start Goma")
        return False

    # On Posix
    goma_script = os.path.join(options.goma_path, 'goma_ctl.py')
    if IsGomaRunning(options, ['python', goma_script, 'status']):
        return True

    stonesthrow.CheckCall(['python', goma_script, 'ensure_start'])
    return True



class Commands:

    def Prepare_Command(self, options):
        """Prepare build directory"""

        InvokeMb(options, "gen")

    @Argument('targets', nargs=argparse.REMAINDER, metavar='TARGETS', help='targets to build')
    def Build_Command(self, options):
        """Build specified targets"""

        if len(options.targets) == 0:
            stonesthrow.Error('no targets specified')
            return

        # If Goma fails, don't try to run a build. A suitable error should
        # already have been presented.
        if not EnsureGoma(options):
            return

        command = ['ninja']
        if options.max_build_jobs != 0:
            command += ['-j', str(options.max_build_jobs)]
        command += options.targets
        stonesthrow.CheckCall(command)


    @Argument('--force', action='store_true', help='force')
    @Argument('targets', nargs=argparse.REMAINDER, metavar='TARGETS', help='targets to clean')
    def Clean_Command(self, options):
        """Clean specified targets"""

        print 'cleaning'


def ConfigureFlags(config):
    parser = argparse.ArgumentParser(description="Chromium platform specific subcommands")
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

def Run(options):
    if not hasattr(options, 'method'):
        raise ValueError('invalid command')
    return options.method(options)


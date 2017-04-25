import stonesthrow
import argparse
import os
import platform

def with_arg(*args, **kwargs):
    def wrap(func):
        if 'option_arguments' not in vars(func):
            vars(func)['option_arguments'] = []
        vars(func)['option_arguments'].append((args, kwargs))
        return func
    return wrap

class Commands:

    @staticmethod
    def run_mb(options, *args):
        if len(args) == 0 or not isinstance(options, stonesthrow.Options):
            raise ValueError("first argument should be an Options object")
        if platform.system() == "Windows":
            mb_tool = os.path.join(options.source_path, "tools", "mb", "mb.bat")
        else:
            mb_tool = os.path.join(options.source_path, "tools", "mb", "mb.py")
        command = [mb_tool, args[0], "-c", options.mb_config, "-g", options.goma_path, options.build_path] + list(args[1:])
        stonesthrow.CheckCall(command)

    def prepare_command(options):
        """Prepare build directory"""
        Commands.run_mb(options, "gen")

    @with_arg('targets', nargs=argparse.REMAINDER, metavar='TARGETS', help='targets to build')
    def build_command(options):
        """Build specified targets"""
        print 'building {}'.format(options.targets)

    @with_arg('--force', action='store_true', help='force')
    def clean_command(options):
        """Clean specified targets"""
        print 'cleaning'

def ConfigureFlags(config):
    parser = argparse.ArgumentParser(description="Chromium platform specific subcommands")
    subparsers = parser.add_subparsers()

    for name, value in vars(Commands).items():
        if not name.endswith('_command'):
            continue

        command = name[:-len('_command')]
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

